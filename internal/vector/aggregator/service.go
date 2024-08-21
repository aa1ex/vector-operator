package aggregator

import (
	"context"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (ctrl *Controller) ensureVectorAggregatorService(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("vector-aggregator-service", ctrl.VectorAggregator.Name)
	log.Info("start Reconcile Vector Aggregator Service")
	return k8s.CreateOrUpdateResource(ctx, ctrl.createVectorAggregatorService(), ctrl.Client)
}

func (ctrl *Controller) createVectorAggregatorService() *corev1.Service {
	labels := ctrl.labelsForVectorAggregator()
	annotations := ctrl.annotationsForVectorAggregator()

	return &corev1.Service{
		ObjectMeta: ctrl.objectMetaVectorAggregator(labels, annotations, ctrl.VectorAggregator.Namespace),
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "api",
					Protocol:   "TCP",
					Port:       ApiPort,
					TargetPort: intstr.FromInt32(ApiPort),
				},
			},
			Selector: labels,
		},
	}
}
