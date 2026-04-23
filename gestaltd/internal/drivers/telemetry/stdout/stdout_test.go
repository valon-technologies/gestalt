package stdout

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrometheusResourceMetricsIncludeEnvDefaultsAndPreserveExplicitAttrs(t *testing.T) {
	t.Setenv("OTEL_SERVICE_VERSION", "env-version")
	t.Setenv("GIT_COMMIT_SHA", "env-sha")
	t.Setenv("K_SERVICE", "toolshed-test")
	t.Setenv("K_REVISION", "toolshed-rev-1")
	t.Setenv("K_CONFIGURATION", "toolshed-config")

	envBody := scrapeProviderMetrics(t, yamlConfig{ServiceName: "env-tools"})
	for _, want := range []string{
		`service_name="env-tools"`,
		`service_version="env-version"`,
		`git_commit_sha="env-sha"`,
		`cloud_provider="gcp"`,
		`cloud_platform="gcp_cloud_run"`,
		`faas_name="toolshed-test"`,
		`faas_version="toolshed-rev-1"`,
		`gcp_cloud_run_configuration_name="toolshed-config"`,
	} {
		if !strings.Contains(envBody, want) {
			t.Fatalf("Prometheus metrics missing %s:\n%s", want, envBody)
		}
	}

	explicitBody := scrapeProviderMetrics(t, yamlConfig{
		ServiceName: "valon-tools",
		ResourceAttributes: map[string]string{
			"service.version": "configured-version",
			"git.commit.sha":  "configured-sha",
		},
	})
	for _, want := range []string{
		`target_info`,
		`service_name="valon-tools"`,
		`service_version="configured-version"`,
		`git_commit_sha="configured-sha"`,
		`cloud_provider="gcp"`,
		`cloud_platform="gcp_cloud_run"`,
	} {
		if !strings.Contains(explicitBody, want) {
			t.Fatalf("Prometheus metrics missing %s:\n%s", want, explicitBody)
		}
	}
	for _, unexpected := range []string{
		`service_version="env-version"`,
		`git_commit_sha="env-sha"`,
	} {
		if strings.Contains(explicitBody, unexpected) {
			t.Fatalf("Prometheus metrics unexpectedly include %s:\n%s", unexpected, explicitBody)
		}
	}
}

func scrapeProviderMetrics(t *testing.T, cfg yamlConfig) string {
	t.Helper()

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New telemetry provider: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	counter, err := p.MeterProvider().Meter("resource-test").Int64Counter("resource_test_counter")
	if err != nil {
		t.Fatalf("create counter: %v", err)
	}
	counter.Add(context.Background(), 1)

	rec := httptest.NewRecorder()
	p.PrometheusHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}
