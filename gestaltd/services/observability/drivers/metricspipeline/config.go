package metricspipeline

type Config struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
}

type PrometheusConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type settings struct {
	prometheusEnabled bool
}

func (cfg Config) normalize() settings {
	return settings{
		prometheusEnabled: boolDefault(cfg.Prometheus.Enabled, true),
	}
}

func boolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
