/*
Copyright 2024.

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

package controller

import (
	"context"
	"errors"
	"github.com/kaasops/vector-operator/internal/config"
	"github.com/kaasops/vector-operator/internal/config/configcheck"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"github.com/kaasops/vector-operator/internal/utils/hash"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	"github.com/kaasops/vector-operator/internal/vector/aggregator"
	monitorv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	api_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	observabilityv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
)

var VectorAggregatorReconciliationSourceChannel = make(chan event.GenericEvent)

// VectorAggregatorReconciler reconciles a VectorAggregator object
type VectorAggregatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Clientset            *kubernetes.Clientset
	PipelineCheckWG      *sync.WaitGroup
	PipelineCheckTimeout time.Duration
	ConfigCheckTimeout   time.Duration
	DiscoveryClient      *discovery.DiscoveryClient
}

// +kubebuilder:rbac:groups=observability.kaasops.io,resources=vectoraggregators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=observability.kaasops.io,resources=vectoraggregators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=observability.kaasops.io,resources=vectoraggregators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VectorAggregator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *VectorAggregatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("VectorAggregator", req.NamespacedName)
	log.Info("Waiting pipeline checks")
	if waitPipelineChecks(r.PipelineCheckWG, r.PipelineCheckTimeout) {
		log.Info("Timeout waiting pipeline checks, continue reconcile vector")
	}
	log.Info("Start Reconcile VectorAggregator")

	vectorCR, err := r.findVectorAggregatorCustomResourceInstance(ctx, req)
	if err != nil {
		log.Error(err, "Failed to get Vector")
		return ctrl.Result{}, err
	}
	if vectorCR == nil {
		log.Info("Vector CR not found. Ignoring since object must be deleted")
		return ctrl.Result{}, nil
	}

	return r.createOrUpdateVectorAggregator(ctx, r.Client, r.Clientset, vectorCR, false)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VectorAggregatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	monitoringCRD, err := k8s.ResourceExists(r.DiscoveryClient, monitorv1.SchemeGroupVersion.String(), monitorv1.PodMonitorsKind)
	if err != nil {
		return err
	}
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&observabilityv1alpha1.VectorAggregator{}).
		WatchesRawSource(source.Channel(VectorAggregatorReconciliationSourceChannel, &handler.EnqueueRequestForObject{})).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{})

	if monitoringCRD {
		builder.Owns(&monitorv1.PodMonitor{})
	}

	if err = builder.Complete(r); err != nil {
		return err
	}
	return nil
}

func (r *VectorAggregatorReconciler) findVectorAggregatorCustomResourceInstance(ctx context.Context, req ctrl.Request) (*observabilityv1alpha1.VectorAggregator, error) {
	// fetch the master instance
	vectorCR := &observabilityv1alpha1.VectorAggregator{}
	err := r.Get(ctx, req.NamespacedName, vectorCR)
	if err != nil {
		if api_errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return vectorCR, nil
}

func listVectorAggregatorCustomResourceInstances(ctx context.Context, client client.Client) (vectors []*observabilityv1alpha1.VectorAggregator, err error) {
	vectorlist := observabilityv1alpha1.VectorAggregatorList{}
	err = client.List(ctx, &vectorlist)
	if err != nil {
		return nil, err
	}
	for _, v := range vectorlist.Items {
		vectors = append(vectors, &v)
	}
	return vectors, nil
}

func (r *VectorAggregatorReconciler) createOrUpdateVectorAggregator(ctx context.Context, client client.Client, clientset *kubernetes.Clientset, v *observabilityv1alpha1.VectorAggregator, configOnly bool) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("VectorAggregator", v.Name)
	// Init Controller for Vector Agent
	vaCtrl := aggregator.NewController(v, client, clientset)

	// Get Vector Config file
	pipelines, err := pipeline.GetValidPipelines(ctx, vaCtrl.Client, observabilityv1alpha1.VectorRoleAggregator)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Get Config in Json ([]byte)
	cfg, err := config.BuildAggregatorConfig(config.VectorConfigParams{
		ApiEnabled:        vaCtrl.VectorAggregator.Spec.Api.Enabled,
		PlaygroundEnabled: vaCtrl.VectorAggregator.Spec.Api.Playground,
		InternalMetrics:   vaCtrl.VectorAggregator.Spec.InternalMetrics,
	}, pipelines...)
	if err != nil {
		return ctrl.Result{}, err
	}
	byteCfg, err := cfg.MarshalJSON()
	if err != nil {
		return ctrl.Result{}, err
	}
	cfgHash := hash.Get(byteCfg)

	if !vaCtrl.VectorAggregator.Spec.ConfigCheck.Disabled {
		if vaCtrl.VectorAggregator.Status.LastAppliedConfigHash == nil || *vaCtrl.VectorAggregator.Status.LastAppliedConfigHash != cfgHash {
			configCheck := configcheck.New(
				byteCfg,
				vaCtrl.Client,
				vaCtrl.ClientSet,
				&vaCtrl.VectorAggregator.Spec.VectorCommon,
				vaCtrl.VectorAggregator.Name,
				vaCtrl.VectorAggregator.Namespace,
				r.ConfigCheckTimeout,
			)
			configCheck.Initiator = configcheck.ConfigCheckInitiatorVector
			reason, err := configCheck.Run(ctx)
			if err != nil {
				if errors.Is(err, configcheck.ValidationError) {
					if err := vaCtrl.SetFailedStatus(ctx, reason); err != nil {
						return ctrl.Result{}, err
					}
					log.Error(err, "Invalid config")
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, err
			}
		}
	}

	vaCtrl.ConfigBytes = byteCfg
	vaCtrl.Config = cfg

	// Start Reconcile Vector Agent
	if err := vaCtrl.EnsureVectorAggregator(ctx); err != nil {
		return ctrl.Result{}, err
	}

	if err := vaCtrl.SetLastAppliedPipelineStatus(ctx, &cfgHash); err != nil {
		//TODO: Handle err: Operation cannot be fulfilled on vectors.observability.kaasops.io \"vector-sample\": the object has been modified; please apply your changes to the latest version and try again
		if api_errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	if err := vaCtrl.SetSuccessStatus(ctx); err != nil {
		// TODO: Handle err: Operation cannot be fulfilled on vectors.observability.kaasops.io \"vector-sample\": the object has been modified; please apply your changes to the latest version and try again
		if api_errors.IsConflict(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}