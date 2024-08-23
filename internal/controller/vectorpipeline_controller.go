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
	"github.com/kaasops/vector-operator/internal/vector/agent"
	"github.com/kaasops/vector-operator/internal/vector/aggregator"
	"sync"
	"time"

	vectorv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/config"
	"github.com/kaasops/vector-operator/internal/config/configcheck"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type PipelineReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Temp. Wait this issue - https://github.com/kubernetes-sigs/controller-runtime/issues/452
	Clientset                  *kubernetes.Clientset
	PipelineCheckWG            *sync.WaitGroup
	PipelineDeleteEventTimeout time.Duration
	ConfigCheckTimeout         time.Duration
}

//+kubebuilder:rbac:groups=observability.kaasops.io,resources=vectorpipelines;clustervectorpipelines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=observability.kaasops.io,resources=vectorpipelines/status;clustervectorpipelines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=observability.kaasops.io,resources=vectorpipelines/finalizers;clustervectorpipelines/finalizers,verbs=update

var VectorAgentReconciliationSourceChannel = make(chan event.GenericEvent)

func (r *PipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("Pipeline", req.Name)
	log.Info("start Reconcile Pipeline")

	pipelineCR, err := r.findPipelineCustomResourceInstance(ctx, req)
	if err != nil {
		log.Error(err, "Failed to get Pipeline")
		return ctrl.Result{}, err
	}

	vectorInstances, err := listVectorCustomResourceInstances(ctx, r.Client)
	if err != nil {
		log.Error(err, "Failed to get Instances")
		return ctrl.Result{}, nil
	}

	vectorAggregatorsInstances, err := listVectorAggregatorCustomResourceInstances(ctx, r.Client)
	if err != nil {
		log.Error(err, "Failed to get Instances")
		return ctrl.Result{}, nil
	}

	if len(vectorInstances) == 0 && len(vectorAggregatorsInstances) == 0 {
		log.Info("Vectors not found")
		return ctrl.Result{}, nil
	}

	if pipelineCR == nil {
		log.Info("Pipeline CR not found. Ignoring since object must be deleted")
		for _, vector := range vectorInstances {
			r.PipelineCheckWG.Add(1)
			go func() {
				VectorAgentReconciliationSourceChannel <- event.GenericEvent{Object: vector}
				log.Info("Waiting if other deletions will be done during timeout")
				time.Sleep(r.PipelineDeleteEventTimeout)
				r.PipelineCheckWG.Done()
			}()
		}
		for _, vector := range vectorAggregatorsInstances {
			r.PipelineCheckWG.Add(1)
			go func() {
				VectorAggregatorReconciliationSourceChannel <- event.GenericEvent{Object: vector}
				log.Info("Waiting if other deletions will be done during timeout")
				time.Sleep(r.PipelineDeleteEventTimeout)
				r.PipelineCheckWG.Done()
			}()
		}
		return ctrl.Result{}, nil
	}

	// Check Pipeline hash
	checkResult, err := pipeline.CheckHash(pipelineCR)
	if err != nil {
		return ctrl.Result{}, err
	}
	if checkResult {
		log.Info("Pipeline has no changes. Finish Reconcile Pipeline")
		return ctrl.Result{}, nil
	}

	if pipelineCR.VectorRole() == vectorv1alpha1.VectorRoleAgent {
		for _, vector := range vectorInstances {
			if vector.DeletionTimestamp != nil {
				continue
			}

			// Init Controller for Vector Agent
			vaCtrl := agent.NewController(vector, r.Client, r.Clientset)

			// Get Vector Config file
			byteConfig, err := config.BuildAgentConfig(config.VectorConfigParams{
				ApiEnabled:        vaCtrl.Vector.Spec.Agent.Api.Enabled,
				PlaygroundEnabled: vaCtrl.Vector.Spec.Agent.Api.Playground,
				UseApiServerCache: vaCtrl.Vector.Spec.UseApiServerCache,
				InternalMetrics:   vaCtrl.Vector.Spec.Agent.InternalMetrics,
			}, pipelineCR)
			if err != nil {
				if err := pipeline.SetFailedStatus(ctx, r.Client, pipelineCR, err.Error()); err != nil {
					return ctrl.Result{}, err
				}
				if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, pipelineCR); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, err
			}

			vaCtrl.Config = byteConfig
			r.PipelineCheckWG.Add(1)
			go r.runPipelineCheck(ctx, pipelineCR, vaCtrl)
		}
	} else { // Aggregator

		for _, vector := range vectorAggregatorsInstances {
			if vector.DeletionTimestamp != nil {
				continue
			}

			// Init Controller for Vector Aggregator
			vaCtrl := aggregator.NewController(vector, r.Client, r.Clientset)

			// Get Vector Config file
			cfg, err := config.BuildAggregatorConfig(config.VectorConfigParams{
				ApiEnabled:        vaCtrl.VectorAggregator.Spec.Api.Enabled,
				PlaygroundEnabled: vaCtrl.VectorAggregator.Spec.Api.Playground,
				InternalMetrics:   vaCtrl.VectorAggregator.Spec.InternalMetrics,
			}, pipelineCR)
			if err != nil {
				if err := pipeline.SetFailedStatus(ctx, r.Client, pipelineCR, err.Error()); err != nil {
					return ctrl.Result{}, err
				}
				if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, pipelineCR); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, err
			}

			byteConfig, err := cfg.MarshalJSON()
			// TODO(aa1ex): remove copy paste
			if err != nil {
				if err := pipeline.SetFailedStatus(ctx, r.Client, pipelineCR, err.Error()); err != nil {
					return ctrl.Result{}, err
				}
				if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, pipelineCR); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, err
			}

			vaCtrl.ConfigBytes = byteConfig
			vaCtrl.Config = cfg

			r.PipelineCheckWG.Add(1)
			go r.runPipelineCheckAggregator(ctx, pipelineCR, vaCtrl)
		}
	}

	log.Info("finish Reconcile Pipeline")
	return ctrl.Result{}, nil
}

func (r *PipelineReconciler) findPipelineCustomResourceInstance(ctx context.Context, req ctrl.Request) (pipeline.Pipeline, error) {
	if req.Namespace != "" {
		vp := &vectorv1alpha1.VectorPipeline{}
		err := r.Get(ctx, req.NamespacedName, vp)
		if err != nil {
			return nil, client.IgnoreNotFound(err)
		}
		return vp, nil
	}

	cvp := &vectorv1alpha1.ClusterVectorPipeline{}
	if err := r.Get(ctx, req.NamespacedName, cvp); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return cvp, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *PipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vectorv1alpha1.VectorPipeline{}).
		Watches(&vectorv1alpha1.ClusterVectorPipeline{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *PipelineReconciler) runPipelineCheck(ctx context.Context, p pipeline.Pipeline, vaCtrl *agent.Controller) {
	log := log.FromContext(ctx).WithValues("Pipeline", p.GetName())
	// Init CheckConfig
	configCheck := configcheck.New(
		vaCtrl.Config,
		vaCtrl.Client,
		vaCtrl.ClientSet,
		&vaCtrl.Vector.Spec.Agent.VectorCommon,
		vaCtrl.Vector.Name,
		vaCtrl.Vector.Namespace,
		r.ConfigCheckTimeout,
	)
	configCheck.Initiator = configcheck.ConfigCheckInitiatorPipieline
	defer r.PipelineCheckWG.Done()

	// Start ConfigCheck
	reason, err := configCheck.Run(ctx)
	if reason != "" {
		if err = pipeline.SetFailedStatus(ctx, r.Client, p, reason); err != nil {
			log.Error(err, "Failed to set pipeline status")
			return
		}
		if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, p); err != nil {
			log.Error(err, "Failed to set pipeline status")
			return
		}
		return
	}
	if err != nil {
		log.Error(err, "ConfigCheck error")
		return
	}

	if err = pipeline.SetSuccessStatus(ctx, r.Client, p); err != nil {
		log.Error(err, "Failed to set pipeline status")
		return
	}

	if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, p); err != nil {
		log.Error(err, "Failed to set pipeline status")
		return
	}
	// Start vector reconciliation
	if *p.GetConfigCheckResult() {
		VectorAgentReconciliationSourceChannel <- event.GenericEvent{Object: vaCtrl.Vector}
	}
}

// TODO(aa1ex): copy paste
func (r *PipelineReconciler) runPipelineCheckAggregator(ctx context.Context, p pipeline.Pipeline, vaCtrl *aggregator.Controller) {
	log := log.FromContext(ctx).WithValues("Pipeline", p.GetName())
	// Init CheckConfig
	configCheck := configcheck.New(
		vaCtrl.ConfigBytes,
		vaCtrl.Client,
		vaCtrl.ClientSet,
		&vaCtrl.VectorAggregator.Spec.VectorCommon,
		vaCtrl.VectorAggregator.Name,
		vaCtrl.VectorAggregator.Namespace,
		r.ConfigCheckTimeout,
	)
	configCheck.Initiator = configcheck.ConfigCheckInitiatorPipieline
	defer r.PipelineCheckWG.Done()

	// Start ConfigCheck
	reason, err := configCheck.Run(ctx)
	if reason != "" {
		if err = pipeline.SetFailedStatus(ctx, r.Client, p, reason); err != nil {
			log.Error(err, "Failed to set pipeline status")
			return
		}
		if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, p); err != nil {
			log.Error(err, "Failed to set pipeline status")
			return
		}
		return
	}
	if err != nil {
		log.Error(err, "ConfigCheck error")
		return
	}

	if err = pipeline.SetSuccessStatus(ctx, r.Client, p); err != nil {
		log.Error(err, "Failed to set pipeline status")
		return
	}

	if err = pipeline.SetLastAppliedPipelineStatus(ctx, r.Client, p); err != nil {
		log.Error(err, "Failed to set pipeline status")
		return
	}
	// Start vector reconciliation
	if *p.GetConfigCheckResult() {
		VectorAggregatorReconciliationSourceChannel <- event.GenericEvent{Object: vaCtrl.VectorAggregator}
	}
}
