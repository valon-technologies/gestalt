package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2ECLIHelp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		args      []string
		wantParts []string
		notWant   []string
	}{
		{
			name:      "root",
			args:      []string{"--help"},
			wantParts: []string{"gestaltd validate", "gestaltd init", "gestaltd plugin <command> [flags]", "gestaltd serve", "--locked"},
			notWant:   []string{"gestaltd bundle", "gestaltd dev"},
		},
		{
			name:      "validate",
			args:      []string{"validate", "--help"},
			wantParts: []string{"gestaltd validate"},
		},
		{
			name:      "init",
			args:      []string{"init", "--help"},
			wantParts: []string{"gestaltd init"},
		},
		{
			name:      "plugin",
			args:      []string{"plugin", "--help"},
			wantParts: []string{"gestaltd plugin <command> [flags]"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := exec.Command(gestaltdBin, tc.args...).CombinedOutput()
			if err != nil {
				t.Fatalf("gestaltd %s: %v\n%s", strings.Join(tc.args, " "), err, out)
			}
			for _, want := range tc.wantParts {
				if !strings.Contains(string(out), want) {
					t.Fatalf("expected output to contain %q, got: %s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(string(out), notWant) {
					t.Fatalf("expected output to omit %q, got: %s", notWant, out)
				}
			}
		})
	}
}

func TestE2ECLIRejectsBadArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		args     []string
		wantPart string
	}{
		{
			name:     "unknown flag",
			args:     []string{"--bogus"},
			wantPart: "flag provided but not defined",
		},
		{
			name:     "top level trailing args",
			args:     []string{"--config", "foo.yaml", "extra"},
			wantPart: "unexpected arguments: extra",
		},
		{
			name:     "serve trailing args",
			args:     []string{"serve", "--config", "foo.yaml", "extra"},
			wantPart: "unexpected arguments: extra",
		},
		{
			name:     "validate trailing args",
			args:     []string{"validate", "--config", "foo.yaml", "extra"},
			wantPart: "unexpected arguments: extra",
		},
		{
			name:     "missing validate config",
			args:     []string{"validate", "--config", "nonexistent.yaml"},
			wantPart: "nonexistent.yaml",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := exec.Command(gestaltdBin, tc.args...).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd %s to fail, output: %s", strings.Join(tc.args, " "), out)
			}
			if !strings.Contains(string(out), tc.wantPart) {
				t.Fatalf("expected output to contain %q, got: %s", tc.wantPart, out)
			}
		})
	}
}

func TestE2ECLIValidateWithStrictProviderErrors(t *testing.T) {
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
  encryption_key: test-key
providers:
  broken:
    display_name: Broken
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(string(out), `plugin.source is required`) {
		t.Fatalf("expected provider-source validation error, got: %s", out)
	}
}
