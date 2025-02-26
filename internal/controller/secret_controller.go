package controller

import (
	"context"
	"github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/misc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type SecretReconciler struct {
	client.Client
	Scheme                    *runtime.Scheme
	SecretsToPipelines        *misc.SecretsToPipelines
	SecretsToPipelinesEventCh chan event.GenericEvent
}

func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("Secret", req.NamespacedName)
	log.Info("Start Reconcile secret")
	if pipelines := r.SecretsToPipelines.Get(req.NamespacedName); len(pipelines) > 0 {
		for _, pipeline := range pipelines {
			vp := &v1alpha1.VectorPipeline{}
			vp.Name = pipeline.Name
			vp.Namespace = pipeline.Namespace
			r.SecretsToPipelinesEventCh <- event.GenericEvent{Object: vp}
		}
	}
	return ctrl.Result{}, nil
}

func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}). // TODO: filter by annotation
		Complete(r)
}
