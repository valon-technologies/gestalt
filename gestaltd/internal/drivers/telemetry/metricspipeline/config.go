package metricspipeline

import "time"

const (
	defaultDashboardInterval = time.Minute
	defaultDashboardWindow   = time.Hour
	defaultDashboardTopN     = 5
)

type Config struct {
	Prometheus PrometheusConfig `yaml:"prometheus"`
	Dashboard  DashboardConfig  `yaml:"dashboard"`
}

type PrometheusConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type DashboardConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type settings struct {
	prometheusEnabled bool
	dashboardEnabled  bool
}

func (cfg Config) normalize() settings {
	return settings{
		prometheusEnabled: boolDefault(cfg.Prometheus.Enabled, true),
		dashboardEnabled:  boolDefault(cfg.Dashboard.Enabled, true),
	}
}

func boolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
