package config

import (
	"context"
	"fmt"
	vectorv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/pipeline"
	"github.com/kaasops/vector-operator/internal/utils/k8s"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	goyaml "sigs.k8s.io/yaml"
	"strings"
)

const (
	AgentApiPort = 8686
)

const (
	secretTypeKubernetesSecret = "kubernetes-secret"
	secretTypeFile             = "file" // TODO
	secretTypeExec             = "exec" // TODO
)

type SecretGetter func(ctx context.Context, pipelineNamespace, pipelineName, secretName string) (*corev1.Secret, error)

func BuildAgentConfig(p VectorConfigParams, f SecretGetter, pipelines ...pipeline.Pipeline) (*VectorConfig, []byte, error) {
	cfg, err := buildAgentConfig(p, f, pipelines...)
	if err != nil {
		return nil, nil, err
	}
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	jsonBytes, err := goyaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return nil, nil, err
	}
	return cfg, jsonBytes, nil
}

func buildAgentConfig(params VectorConfigParams, getSecretForPipeline SecretGetter, pipelines ...pipeline.Pipeline) (*VectorConfig, error) {
	cfg := newVectorConfig(params)

	for _, pipeline := range pipelines {
		p := &PipelineConfig{}
		if err := UnmarshalJson(pipeline.GetSpec(), p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal pipeline %s: %w", pipeline.GetName(), err)
		}
		secretData := make(map[string]map[string][]byte)
		if len(p.Secret) > 0 {
			for backendName, backendConf := range p.Secret {
				if t, ok := backendConf["type"]; !ok || t != secretTypeKubernetesSecret {
					return nil, fmt.Errorf("not supported secret type %s", t)
				}
				secretName, ok := backendConf["name"].(string)
				if secretName == "" || !ok {
					return nil, fmt.Errorf("invalid secret name %s", secretName)
				}
				secret, err := getSecretForPipeline(context.TODO(), pipeline.GetNamespace(), pipeline.GetName(), secretName)
				if err != nil {
					return nil, fmt.Errorf("failed to get secret %s/%s: %w", pipeline.GetNamespace(), secretName, err)
				}
				secretData[backendName] = secret.Data
				//prefBackendName := addPrefix(pipeline.GetNamespace(), pipeline.GetName(), backendName)
				//secretBackends[backendName] = prefBackendName
				//cfg.Secret[prefBackendName] = backendConf
			}
		}
		// TODO: cleanup secretsToPipelines
		for k, v := range p.Sources {
			// Validate source
			if _, ok := pipeline.(*vectorv1alpha1.VectorPipeline); ok {
				if v.Type != KubernetesLogsType {
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
			if v.Type == KubernetesLogsType && params.UseApiServerCache {
				v.UseApiServerCache = true
			}
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			//secretAddPrefix(v.Options, secretBackends)
			fillSecrets(v.Options, secretData)
			cfg.Sources[v.Name] = v
		}
		for k, v := range p.Transforms {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			//secretAddPrefix(v.Options, secretBackends)
			fillSecrets(v.Options, secretData)
			cfg.Transforms[v.Name] = v
		}
		for k, v := range p.Sinks {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			//secretAddPrefix(v.Options, secretBackends)
			fillSecrets(v.Options, secretData)
			cfg.Sinks[v.Name] = v
		}
	}

	// Add exporter pipeline
	if params.InternalMetrics && !isExporterSinkExists(cfg.Sinks) {
		cfg.Sources[DefaultInternalMetricsSourceName] = defaultInternalMetricsSource
		cfg.Sinks[DefaultInternalMetricsSinkName] = defaultInternalMetricsSink
	}

	if len(cfg.Sources) == 0 && len(cfg.Sinks) == 0 {
		cfg.PipelineConfig = defaultAgentPipelineConfig
	}

	return cfg, nil
}

//func secretAddPrefix(options map[string]any, secretBackends map[string]string) {
//	for k, v := range options {
//		switch val := v.(type) {
//		case string:
//			if strings.HasPrefix(val, "SECRET[") && strings.HasSuffix(val, "]") {
//				tmp := strings.TrimPrefix(val, "SECRET[")
//				parts := strings.Split(tmp, ".")
//				if len(parts) == 2 {
//					if secret, ok := secretBackends[parts[0]]; ok {
//						options[k] = fmt.Sprintf("SECRET[%s.%s", secret, parts[1])
//					}
//				}
//			}
//		}
//	}
//}

func fillSecrets(options map[string]any, secretData map[string]map[string][]byte) {
	if len(secretData) == 0 {
		return
	}
	for k, v := range options {
		switch val := v.(type) {
		case string:
			if strings.HasPrefix(val, "SECRET[") && strings.HasSuffix(val, "]") {
				tmp := strings.TrimPrefix(val, "SECRET[")
				tmp = strings.TrimSuffix(tmp, "]")
				parts := strings.Split(tmp, ".")
				if len(parts) == 2 {
					if secretBackend, ok := secretData[parts[0]]; ok {
						if secretValue, ok := secretBackend[parts[1]]; ok {
							options[k] = string(secretValue)
						}
					}
				}
			}
		case map[string]any:
			fillSecrets(val, secretData)
		}
	}
}
