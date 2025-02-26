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
	"regexp"
	goyaml "sigs.k8s.io/yaml"
)

const (
	AgentApiPort = 8686
)

const (
	secretTypeKubernetesSecret = "kubernetes_secret"
	secretTypeFile             = "file"
	secretTypeExec             = "exec"
	secretTypeDirectory        = "directory"
	secretTypeAWSSecretManager = "aws_secrets_manager"
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
		secretBackends := make(map[string]string)
		if len(p.Secret) > 0 {
			for backendName, backendConf := range p.Secret {
				backendType := backendConf["type"]
				switch backendType {
				case secretTypeKubernetesSecret:
					{
						secretName, ok := backendConf["name"].(string)
						if secretName == "" || !ok {
							return nil, fmt.Errorf("invalid secret name %s", secretName)
						}
						secret, err := getSecretForPipeline(context.TODO(), pipeline.GetNamespace(), pipeline.GetName(), secretName)
						if err != nil {
							return nil, fmt.Errorf("failed to get secret %s/%s: %w", pipeline.GetNamespace(), secretName, err)
						}
						secretData[backendName] = secret.Data
					}
				case secretTypeFile, secretTypeExec, secretTypeDirectory, secretTypeAWSSecretManager:
					prefBackendName := addPrefix(pipeline.GetNamespace(), pipeline.GetName(), backendName)
					secretBackends[backendName] = prefBackendName
					cfg.Secret[prefBackendName] = backendConf
				default:
					return nil, fmt.Errorf("not supported secret type %s", backendType)
				}
			}
		}
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
			prepareSecrets(v.Options, secretBackends, secretData)
			cfg.Sources[v.Name] = v
		}
		for k, v := range p.Transforms {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			prepareSecrets(v.Options, secretBackends, secretData)
			cfg.Transforms[v.Name] = v
		}
		for k, v := range p.Sinks {
			v.Name = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), k)
			for i, inputName := range v.Inputs {
				v.Inputs[i] = addPrefix(pipeline.GetNamespace(), pipeline.GetName(), inputName)
			}
			prepareSecrets(v.Options, secretBackends, secretData)
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

var secretRegex = regexp.MustCompile(`^SECRET\[(\w+)\.(\w+)]$`)

func prepareSecrets(options map[string]any, secretBackends map[string]string, secretData map[string]map[string][]byte) {
	for k, v := range options {
		switch val := v.(type) {
		case string:
			{
				matches := secretRegex.FindStringSubmatch(val)
				if matches == nil {
					continue
				}
				backendName := matches[1]
				varName := matches[2]
				if newBackendName, ok := secretBackends[backendName]; ok { // file, exec, etc
					options[k] = fmt.Sprintf("SECRET[%s.%s]", newBackendName, varName)
				} else {
					if secretBackend, ok := secretData[backendName]; ok { // kubernetes_secret
						if secretValue, ok := secretBackend[varName]; ok {
							options[k] = string(secretValue)
						}
					}
				}
			}

		case map[string]any:
			prepareSecrets(val, secretBackends, secretData)
		}
	}
}
