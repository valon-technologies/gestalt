package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_ValidateWithMissingConfig(t *testing.T) {
	t.Parallel()

	err := run([]string{"validate", "--config", "nonexistent.yaml"})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	t.Parallel()

	err := run([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestGestaltd_HelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for --help, got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd validate") {
		t.Fatalf("expected usage output containing 'gestaltd validate', got: %s", out)
	}
	if !strings.Contains(string(out), "gestaltd prepare") {
		t.Fatalf("expected usage output containing 'gestaltd prepare', got: %s", out)
	}
	if !strings.Contains(string(out), "gestaltd serve") {
		t.Fatalf("expected usage output containing 'gestaltd serve', got: %s", out)
	}
	if !strings.Contains(string(out), "gestaltd dev") {
		t.Fatalf("expected usage output containing 'gestaltd dev', got: %s", out)
	}
}

func TestGestaltdValidateHelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "validate", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'validate --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd validate") {
		t.Fatalf("expected usage output containing 'gestaltd validate', got: %s", out)
	}
}

func TestGestaltdPrepareHelpExitsCleanly(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("go", "run", ".", "prepare", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'prepare --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd prepare") {
		t.Fatalf("expected usage output containing 'gestaltd prepare', got: %s", out)
	}
}

func TestRun_ServeRejectsTrailingArgs(t *testing.T) {
	t.Parallel()
	err := run([]string{"serve", "--config", "foo.yaml", "extra"})
	if err == nil {
		t.Fatal("expected error for trailing arguments")
	}
}

func TestRun_RejectsTrailingArgs(t *testing.T) {
	t.Parallel()
	err := run([]string{"--config", "foo.yaml", "extra"})
	if err == nil {
		t.Fatal("expected error for trailing arguments")
	}
}

func TestRun_ValidateRejectsTrailingArgs(t *testing.T) {
	t.Parallel()

	err := run([]string{"validate", "--config", "foo.yaml", "extra"})
	if err == nil {
		t.Fatal("expected error for trailing arguments")
	}
}

func TestRun_ValidateWithStrictProviderErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  dev_mode: true
  encryption_key: test-key
integrations:
  broken:
    upstreams:
      - type: http
        url: https://example.com/openapi.json
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := run([]string{"validate", "--config", cfgPath})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), `unknown upstream type "http"`) {
		t.Fatalf("expected unknown upstream type error, got: %v", err)
	}
}
