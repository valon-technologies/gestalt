package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"gopkg.in/yaml.v3"
)

type testTelemetryProvider struct {
	logger *slog.Logger
}

func (p *testTelemetryProvider) Logger() *slog.Logger { return p.logger }
func (p *testTelemetryProvider) TracerProvider() trace.TracerProvider {
	return nooptrace.NewTracerProvider()
}
func (p *testTelemetryProvider) MeterProvider() metric.MeterProvider {
	return noopmetric.NewMeterProvider()
}
func (p *testTelemetryProvider) Shutdown(context.Context) error { return nil }

func TestBootstrapProducesStructuredLogs(t *testing.T) { //nolint:paralleltest // mutates slog.Default

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"alpha": {
			Plugin: &config.PluginDef{
				BaseURL: "https://api.example.test",
				Operations: []config.InlineOperationDef{
					{Name: "list_items", Method: "GET", Path: "/items"},
				},
			},
		},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	output := buf.String()
	if output == "" {
		t.Fatal("expected structured log output from bootstrap, got empty string")
	}

	var foundProviderLog bool
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}

		if _, ok := record["time"]; !ok {
			t.Errorf("log record missing 'time' field: %v", record)
		}
		if _, ok := record["level"]; !ok {
			t.Errorf("log record missing 'level' field: %v", record)
		}
		if _, ok := record["msg"]; !ok {
			t.Errorf("log record missing 'msg' field: %v", record)
		}

		if record["msg"] == "loaded provider" {
			foundProviderLog = true
			if record["provider"] != "alpha" {
				t.Errorf("expected provider=alpha, got provider=%v", record["provider"])
			}
		}
	}

	if !foundProviderLog {
		t.Errorf("did not find 'loaded provider' log line in output:\n%s", output)
	}
}

func TestBootstrapSkippedProviderLogsWarning(t *testing.T) { //nolint:paralleltest // mutates slog.Default

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"broken": {
			Plugin: &config.PluginDef{
				Command: "/nonexistent/path/to/plugin",
			},
		},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, validFactories())
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	<-result.ProvidersReady

	output := buf.String()
	var foundWarning bool
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}

		if record["msg"] == "skipping provider" && record["level"] == "WARN" {
			foundWarning = true
			if record["provider"] != "broken" {
				t.Errorf("expected provider=broken, got provider=%v", record["provider"])
			}
			if _, ok := record["error"]; !ok {
				t.Error("skipping provider log missing 'error' field")
			}
		}
	}

	if !foundWarning {
		t.Errorf("did not find 'skipping provider' WARN log line in output:\n%s", output)
	}
}

func TestBootstrapAuditSinkUsesTelemetryLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := validConfig()
	factories := validFactories()
	factories.Telemetry["test-telemetry"] = func(yaml.Node) (core.TelemetryProvider, error) {
		return &testTelemetryProvider{
			logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
		}, nil
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })
	<-result.ProvidersReady

	result.AuditSink.Log(context.Background(), core.AuditEntry{
		RequestID: "req-allowed",
		Source:    "binding:test-hook",
		UserID:    "user-1",
		Provider:  "alpha",
		Operation: "read",
		Allowed:   true,
	})
	result.AuditSink.Log(context.Background(), core.AuditEntry{
		RequestID: "req-denied",
		Source:    "binding:test-hook",
		UserID:    "user-2",
		Provider:  "alpha",
		Operation: "write",
		Allowed:   false,
		Error:     "access denied",
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 audit log lines, got %d", len(lines))
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("failed to parse first log line: %v", err)
	}
	if first["msg"] != "audit" {
		t.Fatalf("expected first log msg=audit, got %v", first["msg"])
	}
	if first["level"] != "INFO" {
		t.Fatalf("expected first log level=INFO, got %v", first["level"])
	}
	if first["request_id"] != "req-allowed" {
		t.Fatalf("expected first log request_id=req-allowed, got %v", first["request_id"])
	}

	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("failed to parse second log line: %v", err)
	}
	if second["msg"] != "audit" {
		t.Fatalf("expected second log msg=audit, got %v", second["msg"])
	}
	if second["level"] != "WARN" {
		t.Fatalf("expected second log level=WARN, got %v", second["level"])
	}
	if second["request_id"] != "req-denied" {
		t.Fatalf("expected second log request_id=req-denied, got %v", second["request_id"])
	}
}
