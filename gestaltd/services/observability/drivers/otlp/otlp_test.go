package otlp

import (
	"strings"
	"testing"
	"time"
)

func TestProviderTelemetryEnvUsesOTLPConfig(t *testing.T) {
	t.Parallel()

	ratio := 0.25
	p := &Provider{
		providerEnv: buildProviderTelemetryEnv(yamlConfig{
			Endpoint: "otel-collector:4317",
			Protocol: "grpc",
			Insecure: true,
			Headers:  map[string]string{"x-api-key": "secret value="},
			Traces:   tracesConfig{SamplingRatio: &ratio},
			Metrics:  metricsConfig{Interval: (15 * time.Second).String()},
		}),
		providerResourceAttrs: map[string]string{
			"deployment.environment": "prod",
		},
	}

	env := p.ProviderTelemetryEnv("simple")
	if got := env["OTEL_SERVICE_NAME"]; got != "gestalt-provider-simple" {
		t.Fatalf("OTEL_SERVICE_NAME = %q, want provider service name", got)
	}
	if got := env["OTEL_EXPORTER_OTLP_ENDPOINT"]; got != "otel-collector:4317" {
		t.Fatalf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want endpoint", got)
	}
	if got := env["OTEL_EXPORTER_OTLP_PROTOCOL"]; got != "grpc" {
		t.Fatalf("OTEL_EXPORTER_OTLP_PROTOCOL = %q, want grpc", got)
	}
	if got := env["OTEL_EXPORTER_OTLP_INSECURE"]; got != "true" {
		t.Fatalf("OTEL_EXPORTER_OTLP_INSECURE = %q, want true", got)
	}
	if got := env["OTEL_TRACES_SAMPLER"]; got != "parentbased_traceidratio" {
		t.Fatalf("OTEL_TRACES_SAMPLER = %q, want parentbased_traceidratio", got)
	}
	if got := env["OTEL_TRACES_SAMPLER_ARG"]; got != "0.25" {
		t.Fatalf("OTEL_TRACES_SAMPLER_ARG = %q, want 0.25", got)
	}
	if got := env["OTEL_METRIC_EXPORT_INTERVAL"]; got != "15000" {
		t.Fatalf("OTEL_METRIC_EXPORT_INTERVAL = %q, want 15000", got)
	}
	if got := env["OTEL_EXPORTER_OTLP_HEADERS"]; got != "x-api-key=secret%20value%3D" {
		t.Fatalf("OTEL_EXPORTER_OTLP_HEADERS = %q, want encoded header", got)
	}
	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	for _, want := range []string{
		"deployment.environment=prod",
		"gestaltd.provider.name=simple",
		"service.namespace=gestalt-providers",
	} {
		if !strings.Contains(attrs, want) {
			t.Fatalf("OTEL_RESOURCE_ATTRIBUTES = %q, missing %q", attrs, want)
		}
	}
}
