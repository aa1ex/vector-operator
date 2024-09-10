package aggregator

import (
	"context"
	"fmt"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"maps"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

func (ctrl *Controller) ensureVectorAggregatorService(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("vector-aggregator-service", ctrl.VectorAggregator.Name)
	log.Info("start Reconcile Vector Aggregator Service")
	svcs, err := ctrl.createVectorAggregatorService()
	if err != nil {
		return err
	}
	if len(svcs) == 0 {
		return nil
	}
	for _, svc := range svcs {
		if err := k8s.CreateOrUpdateResource(ctx, svc, ctrl.Client); err != nil {
			return err
		}
	}
	return nil
}

func (ctrl *Controller) createVectorAggregatorService() ([]*corev1.Service, error) {
	labels := ctrl.labelsForVectorAggregator()
	annotations := ctrl.annotationsForVectorAggregator()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	keyFromServicePort := func(servicePort corev1.ServicePort) string {
		return fmt.Sprintf("%s/%s", servicePort.TargetPort.String(), servicePort.Protocol)
	}

	m := make(map[string]corev1.ServicePort)

	sourcesPorts := ctrl.Config.GetSourcesPorts()
	for _, port := range sourcesPorts {
		p := keyFromServicePort(port)
		if _, ok := m[p]; !ok {
			m[p] = port
		} else {
			return nil, fmt.Errorf("duplicate source port %s", p)
		}
	}

	k8sEvSvcPorts := ctrl.Config.GetKubernetesEventsServicesPorts()

	svcs := make([]*corev1.Service, 0, len(k8sEvSvcPorts)+1)

	for _, l := range k8sEvSvcPorts {
		ann := make(map[string]string, len(annotations))
		maps.Copy(ann, annotations)
		ann["observability.kaasops.io/k8s-events-namespace"] = l.Namespace

		sp := corev1.ServicePort{
			Protocol:   corev1.Protocol(strings.ToUpper(l.Protocol)),
			Port:       l.Port,
			Name:       l.Name,
			TargetPort: intstr.FromInt32(l.Port),
		}
		// TODO(aa1ex): refactor
		objectMeta := ctrl.objectMetaVectorAggregator(labels, ann, ctrl.VectorAggregator.Namespace)
		objectMeta.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion:         l.Pipeline.GetTypeMeta().APIVersion,
				Kind:               l.Pipeline.GetTypeMeta().Kind,
				Name:               l.Pipeline.GetName(),
				UID:                l.Pipeline.GetUID(),
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			},
		}
		objectMeta.Namespace = l.Pipeline.GetNamespace()
		svc := &corev1.Service{
			ObjectMeta: objectMeta,
			Spec: corev1.ServiceSpec{
				Ports:    []corev1.ServicePort{sp},
				Selector: labels,
			},
		}
		svc.ObjectMeta.Name += "-" + l.Pipeline.GetName() + "-" + l.Namespace + "-" + l.Name
		svcs = append(svcs, svc)
		delete(m, keyFromServicePort(sp))
	}

	ports := make([]corev1.ServicePort, 0, len(m))
	for _, port := range m {
		ports = append(ports, port)
	}

	if ctrl.VectorAggregator.Spec.Api.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "api",
			Protocol:   corev1.ProtocolTCP,
			Port:       ApiPort,
			TargetPort: intstr.FromInt32(ApiPort),
		})
	}

	svcs = append(svcs, &corev1.Service{
		ObjectMeta: ctrl.objectMetaVectorAggregator(labels, annotations, ctrl.VectorAggregator.Namespace),
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: labels,
		},
	})

	return svcs, nil
}
