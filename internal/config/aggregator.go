package config

import (
	"errors"
	"fmt"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"net"
	"strconv"
)

func BuildAggregatorConfig(params VectorConfigParams, pipelines ...pipeline.Pipeline) (*VectorConfig, error) {
	cfg := newVectorConfig(params)

	cfg.Sources = make(map[string]*Source)
	cfg.Transforms = make(map[string]*Transform)
	cfg.Sinks = make(map[string]*Sink)

	cfg.internal.kubernetesEventsListeners = make([]*KubernetesEventsListener, 0)

	for _, pipeline := range pipelines {
		p := &PipelineConfig{}
		if err := UnmarshalJson(pipeline.GetSpec(), p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal pipeline %s: %w", pipeline.GetName(), err)
		}
		for k, v := range p.Sources {
			// TODO(aa1ex): validate source type
			settings := v
			if v.Type == kubernetesEventsType {
				address := v.Options["address"].(string)
				if address == "" {
					return nil, fmt.Errorf("address is empty from %s", pipeline.GetName())
				}
				protocol, _ := v.Options["mode"].(string)
				if protocol == "" {
					protocol = "tcp"
				}
				if protocol != "tcp" && protocol != "udp" {
					return nil, fmt.Errorf("unsupported mode '%s' for %s pipeline", v.Options["mode"], pipeline.GetName())
				}
				_, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, fmt.Errorf("failed to parse address %s: %w", address, err)
				}
				settings = &Source{
					Name: k,
					Type: SocketType,
					Options: map[string]any{
						"mode":    protocol,
						"address": address,
					},
				}
				portN, err := parsePort(port)
				if err != nil {
					return nil, fmt.Errorf("failed to parse port %s: %w", port, err)
				}
				cfg.internal.kubernetesEventsListeners = append(cfg.internal.kubernetesEventsListeners, &KubernetesEventsListener{
					Port:      portN,
					Protocol:  protocol,
					Namespace: pipeline.GetNamespace(),
					Name:      k,
					Pipeline:  pipeline,
				})
			}
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			cfg.Sources[v.Name] = settings
		}
		for k, v := range p.Transforms {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			cfg.Transforms[v.Name] = v
		}
		for k, v := range p.Sinks {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			cfg.Sinks[v.Name] = v
		}
	}

	// Add exporter pipeline
	if params.InternalMetrics && !isExporterSinkExists(cfg.Sinks) {
		cfg.Sources[DefaultInternalMetricsSourceName] = defaultInternalMetricsSource
		cfg.Sinks[DefaultInternalMetricsSinkName] = defaultInternalMetricsSink
	}
	if len(cfg.Sources) == 0 && len(cfg.Sinks) == 0 {
		cfg.PipelineConfig = defaultAggregatorPipelineConfig
	}

	return cfg, nil
}

func parsePort(port string) (int32, error) {
	p, err := strconv.ParseInt(port, 10, 32)
	if err != nil {
		return 0, err
	}
	if p < 0 || p > 65535 {
		return 0, errors.New("port out of range")
	}
	return int32(p), nil
}
