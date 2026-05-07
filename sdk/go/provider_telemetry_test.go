package gestalt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"go.opentelemetry.io/otel"
)

func TestProviderTelemetryFromEnvExportsMetrics(t *testing.T) {
	metricRequests := make(chan string, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if r.URL.Path == "/v1/metrics" && len(body) > 0 {
			select {
			case metricRequests <- r.URL.Path:
			default:
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)

	t.Setenv(proto.EnvProviderTelemetry, "otel")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_METRICS_EXPORTER", "otlp")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "60000")
	t.Setenv("OTEL_SERVICE_NAME", "gestalt-provider-test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := setupProviderTelemetryFromEnv(ctx)
	if err != nil {
		t.Fatalf("setupProviderTelemetryFromEnv: %v", err)
	}
	counter, err := otel.Meter(TelemetryInstrumentationName).Int64Counter("gestaltd.test.provider.telemetry.count")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}
	counter.Add(ctx, 1)
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown telemetry: %v", err)
	}

	select {
	case <-metricRequests:
	default:
		t.Fatal("collector did not receive an OTLP metrics export")
	}
}
