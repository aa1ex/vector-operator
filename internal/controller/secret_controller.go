package controller

import (
	"context"
	"github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/misc"
	corev1 "k8s.io/api/core/v1"
	api_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type SecretReconciler struct {
	client.Client
	Scheme                    *runtime.Scheme
	SecretsToPipelines        *misc.SecretsToPipelines
	SecretsToPipelinesEventCh chan event.GenericEvent
}

func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if pipelines := r.SecretsToPipelines.Get(req.NamespacedName); len(pipelines) > 0 {
		for _, pipeline := range pipelines {
			vp := &v1alpha1.VectorPipeline{}
			vp.Name = pipeline.Name
			vp.Namespace = pipeline.Namespace
			r.SecretsToPipelinesEventCh <- event.GenericEvent{Object: vp}
		}
		secret := &corev1.Secret{}
		err := r.Get(ctx, req.NamespacedName, secret)
		if err != nil {
			if api_errors.IsNotFound(err) {
				r.SecretsToPipelines.DeleteAll(req.NamespacedName)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})). // TODO: filter by annotation
		Complete(r)
}
