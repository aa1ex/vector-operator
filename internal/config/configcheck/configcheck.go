/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package configcheck

import (
	"context"
	"errors"
	api_errors "k8s.io/apimachinery/pkg/api/errors"
	"math/rand"
	"time"

	"github.com/kaasops/vector-operator/internal/utils/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	vectorv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
)

type ConfigCheck struct {
	Config []byte

	Client    client.Client
	ClientSet *kubernetes.Clientset

	Name                     string
	Namespace                string
	Initiator                string
	Image                    string
	ImagePullPolicy          corev1.PullPolicy
	ImagePullSecrets         []corev1.LocalObjectReference
	Envs                     []corev1.EnvVar
	EnvFrom                  []corev1.EnvFromSource
	Hash                     string
	Tolerations              []corev1.Toleration
	Resources                corev1.ResourceRequirements
	SecurityContext          *corev1.PodSecurityContext
	ContainerSecurityContext *corev1.SecurityContext
	CompressedConfig         bool
	ConfigReloaderImage      string
	ConfigReloaderResources  corev1.ResourceRequirements
	ConfigCheckTimeout       time.Duration
	Annotations              map[string]string
}

func New(
	config []byte,
	c client.Client,
	cs *kubernetes.Clientset,
	vc *vectorv1alpha1.VectorCommon,
	name, namespace string,
	timeout time.Duration,
	initiator string,
) *ConfigCheck {
	image := vc.Image
	if vc.ConfigCheck.Image != nil {
		image = *vc.ConfigCheck.Image
	}

	env := vc.Env

	tolerations := vc.Tolerations
	if vc.ConfigCheck.Tolerations != nil {
		tolerations = *vc.ConfigCheck.Tolerations
	}

	resources := vc.Resources
	if vc.ConfigCheck.Resources != nil {
		resources = *vc.ConfigCheck.Resources
	}

	return &ConfigCheck{
		Config:                   config,
		Client:                   c,
		ClientSet:                cs,
		Name:                     name,
		Namespace:                namespace,
		Image:                    image,
		ImagePullPolicy:          vc.ImagePullPolicy,
		ImagePullSecrets:         vc.ImagePullSecrets,
		Envs:                     env,
		EnvFrom:                  vc.EnvFrom,
		Tolerations:              tolerations,
		Resources:                resources,
		SecurityContext:          vc.SecurityContext,
		ContainerSecurityContext: vc.ContainerSecurityContext,
		CompressedConfig:         vc.CompressConfigFile,
		ConfigReloaderImage:      vc.ConfigReloaderImage,
		ConfigReloaderResources:  vc.ConfigReloaderResources,
		ConfigCheckTimeout:       timeout,
		Annotations:              vc.ConfigCheck.Annotations,
		Initiator:                initiator,
	}
}

func (cc *ConfigCheck) Run(ctx context.Context) (string, error) {
	log := log.FromContext(ctx).WithValues("Vector ConfigCheck", cc.Initiator)
	log.Info("================= Started ConfigCheck =================")

	if err := cc.ensureVectorConfigCheckRBAC(ctx); err != nil && !api_errors.IsAlreadyExists(err) { // TODO(aa1ex): error is silenced, is that ok?
		return "", err
	}

	cc.Hash = randStringRunes()

	vectorConfigCheckSecret, err := cc.createVectorConfigCheckConfig(ctx)
	if err != nil {
		return "", err
	}

	vectorConfigCheckPod := cc.createVectorConfigCheckPod()

	defer func() {
		err = cc.cleanup(ctx, vectorConfigCheckSecret)
	}()

	if err = k8s.CreateOrUpdateResource(ctx, vectorConfigCheckSecret, cc.Client); err != nil {
		return "", err
	}

	// Set OwnerReference to pod
	if err = controllerutil.SetOwnerReference(vectorConfigCheckSecret, vectorConfigCheckPod, cc.Client.Scheme()); err != nil {
		return "", err
	}

	err = k8s.CreatePod(ctx, vectorConfigCheckPod, cc.Client)
	if err != nil {
		return "", err
	}

	reason, err := cc.getCheckResult(ctx, vectorConfigCheckPod)
	if err != nil {
		if errors.Is(err, ValidationError) {
			return reason, err
		}
		return "", err
	}

	return reason, err
}

func (cc *ConfigCheck) ensureVectorConfigCheckRBAC(ctx context.Context) error {
	return cc.ensureVectorConfigCheckServiceAccount(ctx)
}

func (cc *ConfigCheck) ensureVectorConfigCheckServiceAccount(ctx context.Context) error {
	vectorAgentServiceAccount := cc.createVectorConfigCheckServiceAccount()

	return k8s.CreateOrUpdateResource(ctx, vectorAgentServiceAccount, cc.Client)
}

func labelsForVectorConfigCheck() map[string]string {
	return map[string]string{
		k8s.ManagedByLabelKey: "vector-operator",
		k8s.NameLabelKey:      "vector-configcheck",
		k8s.ComponentLabelKey: "ConfigCheck",
	}
}

func (cc *ConfigCheck) annotationsForVectorConfigCheck() map[string]string {
	return cc.Annotations
}

func (cc *ConfigCheck) getNameVectorConfigCheck() string {
	n := "configcheck" + "-" + cc.Name + "-" + cc.Hash

	return n
}

func randStringRunes() string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz")

	b := make([]rune, 5)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func (cc *ConfigCheck) getCheckResult(ctx context.Context, pod *corev1.Pod) (reason string, err error) {
	log := log.FromContext(ctx).WithValues("Vector ConfigCheck", pod.Name)
	log.Info("Trying to get configcheck result")

	watcher, err := cc.ClientSet.CoreV1().Pods(cc.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, pod.Name).String(),
		// LabelSelector: labelsForVectorConfigCheck(),
	})

	if err != nil {
		log.Error(err, "cannot create Pod event watcher")
		return "", err
	}

	defer watcher.Stop()

	for {
		select {
		case e := <-watcher.ResultChan():
			if e.Object == nil {
				return "", nil
			}
			pod, ok := e.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			switch e.Type {
			case watch.Modified:
				if pod.DeletionTimestamp != nil {
					continue
				}
				switch pod.Status.Phase {
				case corev1.PodSucceeded:
					log.Info("Config Check completed successfully")
					return "", nil
				case corev1.PodFailed:
					log.Info("Config Check Failed")
					reason, err := k8s.GetPodLogs(ctx, pod, cc.ClientSet)
					if err != nil {
						return "", err
					}
					return reason, ValidationError
				}
			}
		case <-ctx.Done():
			watcher.Stop()
			return "", nil
		case <-time.After(cc.ConfigCheckTimeout):
			watcher.Stop()
			return "", ConfigcheckTimeoutError
		}
	}
}

func (cc *ConfigCheck) cleanup(ctx context.Context, secret *corev1.Secret) error {

	nn := types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}
	secret, err := k8s.GetSecret(ctx, nn, cc.Client)
	if err != nil {
		return err
	}
	if err := k8s.DeleteSecret(ctx, secret, cc.Client); err != nil {
		return err
	}
	return nil
}

func (cc *ConfigCheck) CleanAll(ctx context.Context) error {
	listOpts, err := cc.configCheckListOpts()
	if err != nil {
		return err
	}
	secrets, err := k8s.ListSecret(ctx, cc.Client, listOpts)
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		if err := k8s.DeleteSecret(ctx, &secret, cc.Client); err != nil {
			return err
		}
	}
	return nil
}

func (cc *ConfigCheck) configCheckListOpts() (client.ListOptions, error) {
	configCheckLabels := labelsForVectorConfigCheck()
	var requirements []labels.Requirement
	for k, v := range configCheckLabels {
		r, err := labels.NewRequirement(k, "==", []string{v})
		if err != nil {
			return client.ListOptions{}, err
		}
		requirements = append(requirements, *r)
	}
	labelsSelector := labels.NewSelector().Add(requirements...)

	return client.ListOptions{
		LabelSelector: labelsSelector,
		Namespace:     cc.Namespace,
	}, nil
}
