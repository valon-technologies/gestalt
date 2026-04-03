package main

import (
	"log/slog"
	"os"
	"path/filepath"
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
