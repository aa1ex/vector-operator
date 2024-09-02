package aggregator

import (
	"context"
	"fmt"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"strings"
)

func (ctrl *Controller) ensureVectorAggregatorService(ctx context.Context) error {
	log := log.FromContext(ctx).WithValues("vector-aggregator-service", ctrl.VectorAggregator.Name)
	log.Info("start Reconcile Vector Aggregator Service")
	svc := ctrl.createVectorAggregatorService()
	if svc == nil {
		return nil
	}
	return k8s.CreateOrUpdateResource(ctx, svc, ctrl.Client)
}

func (ctrl *Controller) createVectorAggregatorService() *corev1.Service {
	labels := ctrl.labelsForVectorAggregator()
	annotations := ctrl.annotationsForVectorAggregator()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations["observability.kaasops.io/k8s-events-handler"] = strings.Join(ctrl.Config.GetKubernetesEventsPorts(), ",")

	m := make(map[string]corev1.ServicePort)

	keyFromServicePort := func(servicePort corev1.ServicePort) string {
		return fmt.Sprintf("%s/%s", servicePort.TargetPort.String(), servicePort.Protocol)
	}

	if len(ctrl.VectorAggregator.Spec.Ports) > 0 {
		aggregatorPorts := parsePorts(ctrl.VectorAggregator.Spec.Ports)
		for _, port := range aggregatorPorts {
			m[keyFromServicePort(port)] = port
		}
	}
	autoDiscoveredPorts := ctrl.Config.GetSourcesPorts()
	for _, port := range autoDiscoveredPorts {
		if _, ok := m[keyFromServicePort(port)]; !ok {
			m[keyFromServicePort(port)] = port
		}
	}
	ports := make([]corev1.ServicePort, 0, len(m))
	for _, port := range m {
		ports = append(ports, port)
	}

	if ctrl.VectorAggregator.Spec.Api.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name:       "api",
			Protocol:   "TCP",
			Port:       ApiPort,
			TargetPort: intstr.FromInt32(ApiPort),
		})
	}

	if len(ports) == 0 {
		return nil
	}

	return &corev1.Service{
		ObjectMeta: ctrl.objectMetaVectorAggregator(labels, annotations, ctrl.VectorAggregator.Namespace),
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: labels,
		},
	}
}

func parsePorts(list []string) []corev1.ServicePort {
	var ports []corev1.ServicePort

	for _, port := range list {
		parts := strings.Split(port, ":")
		switch len(parts) {
		case 2:
			p, err := strconv.Atoi(parts[0])
			if err != nil {
				continue
			}
			servicePort := corev1.ServicePort{
				Name:       "port-" + parts[1],
				Protocol:   "TCP",
				Port:       int32(p),
				TargetPort: intstr.FromInt32(int32(p)),
			}
			ports = append(ports, servicePort)
		case 1:
			ports = parsePort(ports, parts)
		}
	}
	return ports
}

func parsePort(ports []corev1.ServicePort, portString []string) []corev1.ServicePort {
	port, err := strconv.Atoi(portString[0])
	if err != nil {
		return ports
	}

	servicePort := corev1.ServicePort{
		Name:       "port-" + portString[0],
		Protocol:   "TCP",
		Port:       int32(port),
		TargetPort: intstr.FromInt32(int32(port)),
	}
	return append(ports, servicePort)
}
