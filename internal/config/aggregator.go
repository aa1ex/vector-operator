package config

import (
	"fmt"
	vectorv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	"github.com/kaasops/vector-operator/internal/vector/aggregator"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/labels"
	goyaml "sigs.k8s.io/yaml"
)

func BuildAggregatorConfig(vaCtrl *aggregator.Controller, pipelines []pipeline.Pipeline) ([]byte, error) {
	config, err := buildAggregatorConfig(vaCtrl, pipelines...)
	if err != nil {
		return nil, err
	}
	yaml_byte, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	json_byte, err := goyaml.YAMLToJSON(yaml_byte)
	if err != nil {
		return nil, err
	}
	return json_byte, nil
}

func buildAggregatorConfig(vaCtrl *aggregator.Controller, pipelines ...pipeline.Pipeline) (*VectorConfig, error) {
	cfg := newVectorConfig(vaCtrl.VectorAggregator.Spec.Api.Enabled, vaCtrl.VectorAggregator.Spec.Api.Playground)

	cfg.Sources = make(map[string]*Source)
	cfg.Transforms = make(map[string]*Transform)
	cfg.Sinks = make(map[string]*Sink)

	for _, pipeline := range pipelines {
		p := &PipelineConfig{}
		if err := UnmarshalJson(pipeline.GetSpec(), p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal pipeline %s: %w", pipeline.GetName(), err)
		}
		for k, v := range p.Sources {
			// Validate source
			if _, ok := pipeline.(*vectorv1alpha1.VectorPipeline); ok {
				// TODO(aa1ex): validate source types
				_, err := labels.Parse(v.ExtraLabelSelector)
				if err != nil {
					return nil, fmt.Errorf("invalid pod selector for source %s: %w", k, err)
				}
				_, err = labels.Parse(v.ExtraNamespaceLabelSelector)
				if err != nil {
					return nil, fmt.Errorf("invalid namespace selector for source %s: %w", k, err)
				}
				if v.ExtraNamespaceLabelSelector == "" {
					v.ExtraNamespaceLabelSelector = k8s.NamespaceNameToLabel(pipeline.GetNamespace())
				}
				if v.ExtraNamespaceLabelSelector != k8s.NamespaceNameToLabel(pipeline.GetNamespace()) {
					return nil, ErrClusterScopeNotAllowed
				}
			}
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			cfg.Sources[v.Name] = v
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
	if vaCtrl.VectorAggregator.Spec.InternalMetrics && !isExporterSinkExists(cfg.Sinks) {
		cfg.Sources[DefaultInternalMetricsSourceName] = defaultInternalMetricsSource
		cfg.Sinks[DefaultInternalMetricsSinkName] = defaultInternalMetricsSink
	}

	//if len(cfg.Sources) == 0 && len(cfg.Sinks) == 0 {
	//	cfg.PipelineConfig = defaultPipelineConfig
	//}

	return cfg, nil
}
