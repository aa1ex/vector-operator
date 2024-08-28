package config

import (
	"fmt"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"net"
)

func BuildAggregatorConfig(params VectorConfigParams, pipelines ...pipeline.Pipeline) (*VectorConfig, []string, error) {
	cfg := newVectorConfig(params)

	cfg.Sources = make(map[string]*Source)
	cfg.Transforms = make(map[string]*Transform)
	cfg.Sinks = make(map[string]*Sink)

	kubernetesEventsPorts := make([]string, 0)

	for _, pipeline := range pipelines {
		p := &PipelineConfig{}
		if err := unmarshalJson(pipeline.GetSpec(), p); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal pipeline %s: %w", pipeline.GetName(), err)
		}
		for k, v := range p.Sources {
			// TODO(aa1ex): validate source types
			settings := v
			if v.Type == kuberneteEventsType {
				address := v.Options["address"].(string)
				settings = &Source{
					Name: k,
					Type: SocketType,
					Options: map[string]any{
						"mode":    "tcp", // TODO(aa1ex): hardcode
						"address": address,
					},
				}
				_, port, _ := net.SplitHostPort(address) // TODO(aa1ex): handle error
				kubernetesEventsPorts = append(kubernetesEventsPorts, port)
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

	return cfg, kubernetesEventsPorts, nil
}
