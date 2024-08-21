package config

import (
	"fmt"
	vectorv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	"github.com/kaasops/vector-operator/internal/vector/vectoragent"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/labels"
	goyaml "sigs.k8s.io/yaml"
)

func BuildAgentConfig(vaCtrl *vectoragent.Controller, pipelines ...pipeline.Pipeline) ([]byte, error) {
	config, err := buildAgentConfig(vaCtrl, pipelines...)
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

func buildAgentConfig(vaCtrl *vectoragent.Controller, pipelines ...pipeline.Pipeline) (*VectorConfig, error) {
	config := newVectorConfig(vaCtrl.Vector.Spec.Agent.Api.Enabled, vaCtrl.Vector.Spec.Agent.Api.Playground)

	for _, pipeline := range pipelines {
		p := &PipelineConfig{}
		if err := UnmarshalJson(pipeline.GetSpec(), p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal pipeline %s: %w", pipeline.GetName(), err)
		}
		for k, v := range p.Sources {
			// Validate source
			if _, ok := pipeline.(*vectorv1alpha1.VectorPipeline); ok {
				if v.Type != KubernetesSourceType {
					return nil, ErrNotAllowedSourceType
				}
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
			if v.Type == KubernetesSourceType && vaCtrl.Vector.Spec.UseApiServerCache {
				v.UseApiServerCache = true
			}
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			config.Sources[v.Name] = v
		}
		for k, v := range p.Transforms {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			config.Transforms[v.Name] = v
		}
		for k, v := range p.Sinks {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			config.Sinks[v.Name] = v
		}
	}

	// Add exporter pipeline
	if vaCtrl.Vector.Spec.Agent.InternalMetrics && !isExporterSinkExists(config.Sinks) {
		config.Sources[DefaultInternalMetricsSourceName] = defaultInternalMetricsSource
		config.Sinks[DefaultInternalMetricsSinkName] = defaultInternalMetricsSink
	}

	if len(config.Sources) == 0 && len(config.Sinks) == 0 {
		config.PipelineConfig = defaultPipelineConfig
	}

	return config, nil
}
