package config

const (
	// types
	KubernetesLogsType        = "kubernetes_logs"
	kuberneteEventsType       = "kubernetes_events"
	BlackholeSinkType         = "blackhole"
	InternalMetricsSourceType = "internal_metrics"
	PrometheusExporterType    = "prometheus_exporter"
	VectorType                = "vector"
	SocketType                = "socket"

	// default names
	DefaultSourceName                = "defaultSource"
	DefaultSinkName                  = "defaultSink"
	DefaultInternalMetricsSourceName = "internalMetricsSource"
	DefaultInternalMetricsSinkName   = "internalMetricsSink"
)

var (
	defaultAgentSource = &Source{
		Name: DefaultSourceName,
		Type: KubernetesLogsType,
	}
	defaultAggregatorSource = &Source{
		Name: DefaultSourceName,
		Type: VectorType,
		Options: map[string]any{
			"address": "0.0.0.0:8989",
		},
	}
	defaultSink = &Sink{
		Name:   DefaultSinkName,
		Type:   BlackholeSinkType,
		Inputs: []string{DefaultSourceName},
		Options: map[string]interface{}{
			"rate":                100,
			"print_interval_secs": 60,
		},
	}
	defaultAgentPipelineConfig = PipelineConfig{
		Sources: map[string]*Source{
			DefaultSourceName: defaultAgentSource,
		},
		Sinks: map[string]*Sink{
			DefaultSinkName: defaultSink,
		},
	}
	defaultAggregatorPipelineConfig = PipelineConfig{
		Sources: map[string]*Source{
			DefaultSourceName: defaultAggregatorSource,
		},
		Sinks: map[string]*Sink{
			DefaultSinkName: defaultSink,
		},
	}

	defaultInternalMetricsSource = &Source{
		Name: DefaultInternalMetricsSourceName,
		Type: InternalMetricsSourceType,
	}
	defaultInternalMetricsSink = &Sink{
		Name:   DefaultInternalMetricsSinkName,
		Type:   PrometheusExporterType,
		Inputs: []string{DefaultInternalMetricsSourceName},
	}
)
