package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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
			wantParts: []string{"gestaltd validate", "gestaltd lock", "gestaltd sync [--locked]", "gestaltd agent <command> [flags]", "gestaltd provider <command> [flags]", "gestaltd serve", "--locked", "--no-sync", "[--config PATH]...", "--lockfile PATH"},
			notWant:   []string{"gestaltd lock [--config PATH]... [--lockfile PATH] [--platform", "gestaltd bundle", "gestaltd dev", "gestaltd init", "\n  init"},
		},
		{
			name:      "validate",
			args:      []string{"validate", "--help"},
			wantParts: []string{"gestaltd validate", "--lockfile PATH", "--platform os/arch"},
		},
		{
			name:      "lock",
			args:      []string{"lock", "--help"},
			wantParts: []string{"gestaltd lock", "write canonical lock metadata", "--check"},
			notWant:   []string{"--platform"},
		},
		{
			name:      "sync",
			args:      []string{"sync", "--help"},
			wantParts: []string{"gestaltd sync [--locked]", "Materialize prepared artifacts", "--artifacts-dir", "--parallelism", "--cache-dir", "--output-format", "-v, --verbose", "--check"},
		},
		{
			name:      "provider",
			args:      []string{"provider", "--help"},
			wantParts: []string{"gestaltd provider <command> [flags]", "add", "info", "list", "remove", "repo", "search", "upgrade", "validate", "release"},
			notWant:   []string{"  dev         ", "attach"},
		},
		{
			name:      "serve",
			args:      []string{"serve", "--help"},
			wantParts: []string{"gestaltd serve [PATH]", "--port PORT", "--no-sync", "run:"},
		},
		{
			name:      "provider repo",
			args:      []string{"provider", "repo", "--help"},
			wantParts: []string{"gestaltd provider repo <command> [flags]", "add", "list", "remove", "update"},
		},
		{
			name:      "provider validate",
			args:      []string{"provider", "validate", "--help"},
			wantParts: []string{"gestaltd provider validate", "v1 supports kind: app manifests", "--config PATH"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := gestaltdCommand(tc.args...).CombinedOutput()
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

func TestRunLockRejectsPlatformFlag(t *testing.T) {
	t.Parallel()

	out, err := gestaltdCommand("lock", "--platform", "linux/amd64").CombinedOutput()
	if err == nil {
		t.Fatalf("gestaltd lock --platform unexpectedly succeeded:\n%s", out)
	}
	if !strings.Contains(string(out), "flag provided but not defined: -platform") {
		t.Fatalf("gestaltd lock --platform output = %s, want unknown flag error", out)
	}
}

func TestE2ECLITopLevelVersionShortFlag(t *testing.T) {
	t.Parallel()

	out, err := gestaltdCommand("-v").CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd -v: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got == "" {
		t.Fatalf("gestaltd -v output is empty")
	}
}

func TestE2ESyncJSONStdoutCleanWithSourceBuildOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	buildScriptPath := filepath.Join(indexedDBDir, "build.sh")
	buildScript, err := os.ReadFile(buildScriptPath)
	if err != nil {
		t.Fatalf("read build script: %v", err)
	}
	if err := os.WriteFile(buildScriptPath, []byte("printf 'BUILD_STDOUT_SENTINEL\\n'\n"+string(buildScript)), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v8
server:
  baseUrl: %s
  encryptionKey: test-e2e-key
  providers:
    indexeddb: sqlite
  artifactsDir: %s
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
`, e2eLoopbackBaseURL(18080), artifactsDir, indexedDBManifest, filepath.Join(dir, "gestalt.db"))
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	unrelated, err := os.Create(filepath.Join(dir, "unrelated-large.bin"))
	if err != nil {
		t.Fatalf("create unrelated sparse file: %v", err)
	}
	if err := unrelated.Truncate(1 << 30); err != nil {
		_ = unrelated.Close()
		t.Fatalf("truncate unrelated sparse file: %v", err)
	}
	if err := unrelated.Close(); err != nil {
		t.Fatalf("close unrelated sparse file: %v", err)
	}

	out, err := gestaltdCommand("lock", "--config", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(artifactsDir, "indexeddb", "sqlite")); err != nil {
		t.Fatalf("remove prepared indexeddb provider: %v", err)
	}

	cmd := gestaltdCommand("sync", "--locked", "--output-format=json", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gestaltd sync json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "BUILD_STDOUT_SENTINEL") {
		t.Fatalf("JSON stdout contained source build output:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "BUILD_STDOUT_SENTINEL") {
		t.Fatalf("stderr missing source build output sentinel:\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	var doc syncOutputDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("sync stdout is not JSON: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if doc.Command != "sync" || doc.Sync.Action != "materialize" {
		t.Fatalf("sync JSON command/action = %q/%q, want sync/materialize", doc.Command, doc.Sync.Action)
	}
	if !doc.Output.Measured {
		t.Fatalf("sync JSON output.measured = false, want true")
	}
	if doc.Output.Bytes >= 1<<30 {
		t.Fatalf("sync JSON output bytes = %d, want unrelated artifacts-dir file excluded", doc.Output.Bytes)
	}

	stdout.Reset()
	stderr.Reset()
	cmd = gestaltdCommand("sync", "--locked", "--check", "--output-format=json", "--config", configPath)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gestaltd sync check json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	doc = syncOutputDocument{}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("sync check stdout is not JSON: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if doc.Sync.Action != "check" || !doc.Inputs.Check {
		t.Fatalf("sync check JSON action/check = %q/%t, want check/true", doc.Sync.Action, doc.Inputs.Check)
	}
	if doc.Output.Measured {
		t.Fatalf("sync check JSON output.measured = true, want false")
	}
	if doc.Cache.Put.Successes != 0 || doc.Cache.Put.Failures != 0 {
		t.Fatalf("sync check JSON cache put successes/failures = %d/%d, want 0/0", doc.Cache.Put.Successes, doc.Cache.Put.Failures)
	}
}

func TestE2ESyncDefaultSuccessReportsProgressAndQuietSuppressesIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)
	buildScriptPath := filepath.Join(appDir, "build.sh")
	buildScript, err := os.ReadFile(buildScriptPath)
	if err != nil {
		t.Fatalf("read build script: %v", err)
	}
	if err := os.WriteFile(buildScriptPath, []byte("printf 'DEFAULT_SYNC_STDOUT_SENTINEL\\n'\nprintf 'DEFAULT_SYNC_STDERR_SENTINEL\\n' >&2\n"+string(buildScript)), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	configPath := writeE2EConfig(t, dir, appDir, 18080)

	out, err := gestaltdCommand("lock", "--config", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(dir, "providers", "example")); err != nil {
		t.Fatalf("remove prepared provider: %v", err)
	}

	cmd := gestaltdCommand("sync", "--locked", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("gestaltd sync: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("default sync stdout = %q, want empty", got)
	}
	for _, want := range []string{"Loading configuration", "Resolving lockfile", "Preparing artifacts", "Artifacts prepared"} {
		if got := stderr.String(); !strings.Contains(got, want) {
			t.Fatalf("default sync stderr missing %q: %q", want, got)
		}
	}
	if strings.Contains(stderr.String(), "DEFAULT_SYNC_") {
		t.Fatalf("default sync leaked subprocess output into lifecycle summary: %q", stderr.String())
	}

	if err := os.RemoveAll(filepath.Join(dir, "providers", "example")); err != nil {
		t.Fatalf("remove prepared provider for quiet sync: %v", err)
	}
	quiet := gestaltdCommand("sync", "--quiet", "--locked", "--config", configPath)
	stdout.Reset()
	stderr.Reset()
	quiet.Stdout = &stdout
	quiet.Stderr = &stderr
	if err := quiet.Run(); err != nil {
		t.Fatalf("quiet gestaltd sync: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("quiet sync output stdout=%q stderr=%q, want both empty", stdout.String(), stderr.String())
	}
}

func TestE2ESyncQuietFailedBuildShowsDiagnostics(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	buildScriptPath := filepath.Join(indexedDBDir, "build.sh")
	if err := os.WriteFile(buildScriptPath, []byte("#!/bin/sh\nprintf 'QUIET_FAIL_STDOUT\\n'\nprintf 'QUIET_FAIL_STDERR\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v8
server:
  baseUrl: %s
  encryptionKey: test-e2e-key
  providers:
    indexeddb: sqlite
  artifactsDir: %s
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
`, e2eLoopbackBaseURL(18080), artifactsDir, indexedDBManifest, filepath.Join(dir, "gestalt.db"))
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := gestaltdCommand("lock", "--config", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(artifactsDir, "indexeddb", "sqlite")); err != nil {
		t.Fatalf("remove prepared indexeddb provider: %v", err)
	}

	cmd := gestaltdCommand("--quiet", "sync", "--locked", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("gestaltd sync --quiet unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("quiet failed sync stdout = %q, want empty", got)
	}
	errOut := stderr.String()
	for _, want := range []string{"QUIET_FAIL_STDOUT", "QUIET_FAIL_STDERR"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("quiet failed sync stderr = %q, want %q\nstdout:\n%s", errOut, want, stdout.String())
		}
	}
}

func TestE2ESyncQuietJSONFailedBuildShowsDiagnosticsOnStderr(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	buildScriptPath := filepath.Join(indexedDBDir, "build.sh")
	if err := os.WriteFile(buildScriptPath, []byte("#!/bin/sh\nprintf 'QUIET_JSON_FAIL_STDOUT\\n'\nprintf 'QUIET_JSON_FAIL_STDERR\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write build script: %v", err)
	}
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	artifactsDir := filepath.Join(dir, "artifacts")
	configPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v8
server:
  baseUrl: %s
  encryptionKey: test-e2e-key
  providers:
    indexeddb: sqlite
  artifactsDir: %s
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
`, e2eLoopbackBaseURL(18080), artifactsDir, indexedDBManifest, filepath.Join(dir, "gestalt.db"))
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := gestaltdCommand("lock", "--config", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock: %v\n%s", err, out)
	}
	if err := os.RemoveAll(filepath.Join(artifactsDir, "indexeddb", "sqlite")); err != nil {
		t.Fatalf("remove prepared indexeddb provider: %v", err)
	}

	cmd := gestaltdCommand("--quiet", "sync", "--locked", "--output-format=json", "--config", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("gestaltd sync --quiet --output-format=json unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Fatalf("quiet failed JSON sync stdout = %q, want empty\nstderr:\n%s", got, stderr.String())
	}
	errOut := stderr.String()
	for _, want := range []string{"QUIET_JSON_FAIL_STDOUT", "QUIET_JSON_FAIL_STDERR"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("quiet failed JSON sync stderr = %q, want %q\nstdout:\n%s", errOut, want, stdout.String())
		}
	}
}

func TestE2ESyncJSONFailureLeavesStdoutEmpty(t *testing.T) {
	t.Parallel()

	missingConfig := filepath.Join(t.TempDir(), "missing.yaml")
	cmd := gestaltdCommand("sync", "--locked", "--output-format=json", "--config", missingConfig)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("gestaltd sync json with missing config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "" {
		t.Fatalf("failed JSON sync stdout = %q, want empty\nstderr:\n%s", got, stderr.String())
	}
}

func TestE2EProviderAddPackageSourceUpdatesConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gestalt.yaml")
	if err := os.WriteFile(cfgPath, []byte("apiVersion: gestaltd.config/v8\napps:\n"), 0o644); err != nil {
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
        kind: app
        runtime: executable
`
	if err := os.WriteFile(indexPath, []byte(indexYAML), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	indexURL := (&url.URL{Scheme: "file", Path: indexPath}).String()

	out, err := gestaltdCommand("provider", "repo", "add", "local", indexURL, "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("provider repo add failed: %v\n%s", err, out)
	}
	out, err = gestaltdCommand("provider", "add", "github.com/acme/providers/alpha", "--config", cfgPath, "--repo", "local", "--name", "alpha", "--no-lock").CombinedOutput()
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
	entry := cfg.Apps["alpha"]
	if entry == nil {
		t.Fatal(`Apps["alpha"] = nil`)
		return
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
			name:     "serve rejects legacy path flag",
			args:     []string{"serve", "--path", "."},
			wantPart: "flag provided but not defined: -path",
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
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, err := gestaltdCommand(tc.args...).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd %s to fail, output: %s", strings.Join(tc.args, " "), out)
			}
			if !strings.Contains(string(out), tc.wantPart) {
				t.Fatalf("expected output to contain %q, got: %s", tc.wantPart, out)
			}
		})
	}
}
