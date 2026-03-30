package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestBootstrapProducesStructuredLogs(t *testing.T) { //nolint:paralleltest // mutates slog.Default

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	result, err := bootstrap.Bootstrap(context.Background(), validConfig(), validFactories())
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
			if _, ok := record["operations"]; !ok {
				t.Error("loaded provider log missing 'operations' field")
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
		"broken": {},
	}
	factories := validFactories()
	factories.Providers = map[string]bootstrap.ProviderFactory{
		"broken": func(_ context.Context, _ string, _ config.IntegrationDef, _ bootstrap.Deps) (*bootstrap.ProviderBuildResult, error) {
			return nil, context.DeadlineExceeded
		},
	}

	result, err := bootstrap.Bootstrap(context.Background(), cfg, factories)
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
