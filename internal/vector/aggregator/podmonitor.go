package aggregator

import (
	"context"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	monitorv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (ctrl *Controller) ensureVectorAggregatorPodMonitor(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("vector-aggregator-podmonitor", ctrl.VectorAggregator.Name)
	log.Info("start Reconcile Vector Aggregator PodMonitor")
	vectorAggregatorPodMonitor := ctrl.createVectorAggregatorPodMonitor()
	return k8s.CreateOrUpdateResource(ctx, vectorAggregatorPodMonitor, ctrl.Client)
}

func (ctrl *Controller) createVectorAggregatorPodMonitor() *monitorv1.PodMonitor {
	labels := ctrl.labelsForVectorAggregator()
	annotations := ctrl.annotationsForVectorAggregator()

	podmonitor := &monitorv1.PodMonitor{
		ObjectMeta: ctrl.objectMetaVectorAggregator(labels, annotations, ctrl.VectorAggregator.Namespace),
		Spec: monitorv1.PodMonitorSpec{
			PodMetricsEndpoints: []monitorv1.PodMetricsEndpoint{
				{
					Path: "/metrics",
					Port: "prom-exporter",
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
		},
	}

	return podmonitor
}
