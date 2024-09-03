package controller

import (
	"context"
	"fmt"
	"github.com/kaasops/vector-operator/internal/k8sevents"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"strings"
)

const (
	k8sEventsAnnotation = "observability.kaasops.io/k8s-events-handler"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	EventsManager *k8sevents.EventsManager
}

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Start Reconcile Service")

	svc := &corev1.Service{}
	err := r.Get(ctx, req.NamespacedName, svc)
	if err != nil {
		// TODO(aa1ex): deleteSubscriber
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	value, _ := svc.Annotations[k8sEventsAnnotation]
	if value != "" {
		list := strings.Split(value, ",")
		for _, v := range list {
			rec := strings.Split(v, ":")
			ns := rec[0]
			parts := strings.Split(rec[1], "/")
			r.EventsManager.RegisterSubscriber(fmt.Sprintf("%s.%s", svc.Name, svc.Namespace), parts[0], parts[1], ns)
		}
	}

	return ctrl.Result{}, nil
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
				return hasRequiredAnnotation(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return hasRequiredAnnotation(e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return hasRequiredAnnotation(e.Object)
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return hasRequiredAnnotation(e.Object)
			},
		}).
		Complete(r)
}

func hasRequiredAnnotation(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	_, found := annotations[k8sEventsAnnotation]
	return found
}
