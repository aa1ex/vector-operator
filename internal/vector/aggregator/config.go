package aggregator

import (
	"context"
	"github.com/kaasops/vector-operator/internal/utils/compression"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (ctrl *Controller) ensureVectorAggregatorConfig(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("vector-aggregator-secret", ctrl.VectorAggregator.Name)
	log.Info("start Reconcile Vector Aggregator Secret")
	vectorAggregatorSecret, err := ctrl.createVectorAggregatorConfig(ctx)
	if err != nil {
		return err
	}
	return k8s.CreateOrUpdateResource(ctx, vectorAggregatorSecret, ctrl.Client)
}

func (ctrl *Controller) createVectorAggregatorConfig(ctx context.Context) (*corev1.Secret, error) {
	log := log.FromContext(ctx).WithValues("vector-aggregator-config", ctrl.VectorAggregator.Name)
	labels := ctrl.labelsForVectorAggregator()
	annotations := ctrl.annotationsForVectorAggregator()
	data := ctrl.Config

	if ctrl.VectorAggregator.Spec.CompressConfigFile {
		data = compression.Compress(ctrl.Config, log)
	}
	config := map[string][]byte{
		"aggregator.json": data,
	}
	secret := &corev1.Secret{
		ObjectMeta: ctrl.objectMetaVectorAggregator(labels, annotations, ctrl.VectorAggregator.Namespace),
		Data:       config,
	}
	return secret, nil
}
