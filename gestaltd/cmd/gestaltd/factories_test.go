package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSetupBootstrap_InstallsTelemetryLoggerAndRestoresItOnClose(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
telemetry:
  provider: stdout
  config:
    format: json
server:
  encryption_key: test-key
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prev := slog.Default()
	env, err := setupBootstrap(cfgPath, false)
	if err != nil {
		t.Fatalf("setupBootstrap: %v", err)
	}
	if slog.Default() != env.Result.Telemetry.Logger() {
		t.Fatal("expected setupBootstrap to install the telemetry logger as slog.Default")
	}

	env.Close()

	if slog.Default() != prev {
		t.Fatal("expected bootstrapEnv.Close to restore the previous slog.Default logger")
	}
}

func TestSetupBootstrap_RestoresTelemetryLoggerOnMigrateFailure(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	dir := t.TempDir()
	lockedDir := filepath.Join(dir, "locked")
	if err := os.Mkdir(lockedDir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(lockedDir, 0o500); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(lockedDir, 0o755)
	})

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(lockedDir, "gestalt.db") + `
telemetry:
  provider: stdout
  config:
    format: json
server:
  encryption_key: test-key
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prev := slog.Default()
	env, err := setupBootstrap(cfgPath, false)
	if env != nil {
		env.Close()
		t.Fatal("expected setupBootstrap to fail during migration")
	}
	if err == nil {
		t.Fatal("expected migration error")
	}
	if slog.Default() != prev {
		t.Fatal("expected setupBootstrap to restore slog.Default after migration failure")
	}
}

func TestSetupBootstrap_OTLPKeepsLogsOnStdout(t *testing.T) { //nolint:paralleltest // mutates slog.Default and os.Stdout
	dir := t.TempDir()

	var (
		mu       sync.Mutex
		requests []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()

		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
telemetry:
  provider: otlp
  config:
    endpoint: ` + strings.TrimPrefix(srv.URL, "http://") + `
    protocol: http
    insecure: true
    logs:
      exporter: stdout
      format: json
server:
  encryption_key: test-key
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = reader.Close()
	})

	prev := slog.Default()
	env, err := setupBootstrap(cfgPath, false)
	if err != nil {
		t.Fatalf("setupBootstrap: %v", err)
	}
	if slog.Default() != env.Result.Telemetry.Logger() {
		t.Fatal("expected setupBootstrap to install the telemetry logger as slog.Default")
	}

	env.Result.Telemetry.Logger().Info("hybrid-telemetry-log", "source", "test")
	env.Close()

	_ = writer.Close()
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	got := string(output)
	if !strings.Contains(got, `"msg":"hybrid-telemetry-log"`) {
		t.Fatalf("expected stdout log output, got %q", got)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range requests {
		if path == "/v1/logs" {
			t.Fatalf("expected stdout logging path, but saw OTLP log export request %q", path)
		}
	}

	if slog.Default() != prev {
		t.Fatal("expected bootstrapEnv.Close to restore the previous slog.Default logger")
	}
}
