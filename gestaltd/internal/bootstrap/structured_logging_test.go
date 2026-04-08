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

func TestBootstrapSkippedProviderLogsWarning(t *testing.T) { //nolint:paralleltest // mutates slog.Default

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := validConfig()
	cfg.Integrations = map[string]config.IntegrationDef{
		"broken": {
			Plugin: &config.PluginDef{
				Source: &config.PluginSourceDef{Path: "./plugin.yaml"},
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
