package daemon

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
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
			wantParts: []string{"gestaltd validate", "gestaltd lock", "gestaltd sync --locked", "gestaltd agent <command> [flags]", "gestaltd provider <command> [flags]", "gestaltd serve", "--locked", "[--config PATH]...", "--lockfile PATH"},
			notWant:   []string{"gestaltd bundle", "gestaltd dev", "gestaltd init", "\n  init"},
		},
		{
			name:      "validate",
			args:      []string{"validate", "--help"},
			wantParts: []string{"gestaltd validate", "Repeated --config flags merge left-to-right.", "--lockfile PATH"},
		},
		{
			name:      "lock",
			args:      []string{"lock", "--help"},
			wantParts: []string{"gestaltd lock", "write canonical lock metadata", "--platform", "--check"},
		},
		{
			name:      "sync",
			args:      []string{"sync", "--help"},
			wantParts: []string{"gestaltd sync --locked", "Materialize prepared artifacts", "--artifacts-dir", "--check"},
		},
		{
			name:      "provider",
			args:      []string{"provider", "--help"},
			wantParts: []string{"gestaltd provider <command> [flags]", "add", "attach", "dev", "info", "list", "remove", "repo", "search", "upgrade", "validate", "release"},
		},
		{
			name:      "provider repo",
			args:      []string{"provider", "repo", "--help"},
			wantParts: []string{"gestaltd provider repo <command> [flags]", "add", "list", "remove", "update"},
		},
		{
			name:      "provider attach",
			args:      []string{"provider", "attach", "--help"},
			wantParts: []string{"gestaltd provider attach <command> [flags]", "list", "show", "detach"},
		},
		{
			name:      "provider validate",
			args:      []string{"provider", "validate", "--help"},
			wantParts: []string{"gestaltd provider validate", "v1 supports kind: plugin and kind: ui manifests", "--config PATH"},
		},
		{
			name:      "provider dev",
			args:      []string{"provider", "dev", "--help"},
			wantParts: []string{"gestaltd provider dev", "The built-in admin UI remains available at /admin", "sibling public UIs", "--port PORT"},
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

func TestE2EProviderAddPackageSourceUpdatesConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(cfgPath, []byte("apiVersion: gestaltd.config/v5\nplugins:\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	indexPath := filepath.Join(dir, "provider-index.yaml")
	indexYAML := `
schema: gestaltd-provider-index
schemaVersion: 1
packages:
  github.com/acme/providers/alpha:
    displayName: Alpha
    versions:
      1.2.3:
        metadata: file:///tmp/provider-release.yaml
        kind: plugin
        runtime: executable
`
	if err := os.WriteFile(indexPath, []byte(indexYAML), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	indexURL := (&url.URL{Scheme: "file", Path: indexPath}).String()

	out, err := exec.Command(gestaltdBin, "provider", "repo", "add", "local", indexURL, "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("provider repo add failed: %v\n%s", err, out)
	}
	out, err = exec.Command(gestaltdBin, "provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--no-lock").CombinedOutput()
	if err != nil {
		t.Fatalf("provider add failed: %v\n%s", err, out)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.APIVersion; got != config.ConfigAPIVersion {
		t.Fatalf("APIVersion = %q, want %q", got, config.ConfigAPIVersion)
	}
	if got := cfg.ProviderRepositories["local"].URL; got != indexURL {
		t.Fatalf("providerRepositories.local.url = %q, want %q", got, indexURL)
	}
	entry := cfg.Plugins["alpha"]
	if entry == nil {
		t.Fatal(`Plugins["alpha"] = nil`)
	}
	if got := entry.Source.PackageRepo(); got != "local" {
		t.Fatalf("Source.PackageRepo = %q, want local", got)
	}
	if got := entry.Source.PackageAddress(); got != "github.com/acme/providers/alpha" {
		t.Fatalf("Source.PackageAddress = %q, want package", got)
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
		{
			name:     "provider validate trailing args",
			args:     []string{"provider", "validate", "--path", ".", "extra"},
			wantPart: "unexpected arguments: extra",
		},
		{
			name:     "provider dev trailing args",
			args:     []string{"provider", "dev", "--path", ".", "extra"},
			wantPart: "unexpected arguments: extra",
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
