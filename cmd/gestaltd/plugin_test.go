package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var stdoutMu sync.Mutex

func TestRun_PluginHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "plugin", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd plugin <command> [flags]") {
		t.Fatalf("expected plugin usage output, got: %s", out)
	}
}

func TestRun_PluginPackageHelpExitsCleanly(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "plugin", "package", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 for 'plugin package --help', got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "gestaltd plugin package --input PATH --output PATH") {
		t.Fatalf("expected package usage output, got: %s", out)
	}
}

func TestRun_PluginRootReturnsHelpWhenNoSubcommandProvided(t *testing.T) {
	t.Parallel()

	err := run([]string{"plugin"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got: %v", err)
	}
}

//nolint:paralleltest // Swaps os.Stdout via captureStdout.
func TestRun_PluginPackageCreatesArchive(t *testing.T) {
	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "acme-provider-0.1.0.tar.gz")

	output := captureStdout(t, func() error {
		return run([]string{"plugin", "package", "--input", src, "--output", outPath})
	})

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected package archive to exist: %v", err)
	}
	if !strings.Contains(output, "packaged") {
		t.Fatalf("expected package output, got: %q", output)
	}
}

//nolint:paralleltest // Swaps os.Stdout via captureStdout.
func TestRun_PluginInspectPrintsManifestSummary(t *testing.T) {
	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "acme-provider-0.1.0.tar.gz")
	captureStdout(t, func() error {
		return run([]string{"plugin", "package", "--input", src, "--output", outPath})
	})

	output := captureStdout(t, func() error {
		return run([]string{"plugin", "inspect", outPath})
	})

	if !strings.Contains(output, "id: acme/provider") {
		t.Fatalf("expected manifest summary, got: %q", output)
	}
	if !strings.Contains(output, "artifact:") {
		t.Fatalf("expected artifact summary, got: %q", output)
	}
}

//nolint:paralleltest // Swaps os.Stdout via captureStdout.
func TestRun_PluginInstallAndListUseLocalStore(t *testing.T) {
	dir := t.TempDir()
	src := newPluginPackageFixture(t, dir)
	outPath := filepath.Join(dir, "acme-provider-0.1.0.tar.gz")
	captureStdout(t, func() error {
		return run([]string{"plugin", "package", "--input", src, "--output", outPath})
	})
	configPath := filepath.Join(dir, "config", "gestalt.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatalf("MkdirAll(config): %v", err)
	}
	if err := os.WriteFile(configPath, []byte("auth:\n  provider: google\ndatastore:\n  provider: sqlite\nserver:\n  encryption_key: key123\n"), 0644); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	installOutput := captureStdout(t, func() error {
		return run([]string{"plugin", "install", "--config", configPath, outPath})
	})
	if !strings.Contains(installOutput, "installed acme/provider@0.1.0") {
		t.Fatalf("expected install output, got: %q", installOutput)
	}

	executablePath := filepath.Join(filepath.Dir(configPath), ".gestalt", "plugins", "acme", "provider", "0.1.0", "artifacts", runtime.GOOS, runtime.GOARCH, "provider")
	if _, err := os.Stat(executablePath); err != nil {
		t.Fatalf("expected installed executable at %s: %v", executablePath, err)
	}

	listOutput := captureStdout(t, func() error {
		return run([]string{"plugin", "list", "--config", configPath})
	})
	if !strings.Contains(listOutput, "acme/provider@0.1.0") {
		t.Fatalf("expected list output to contain installed ref, got: %q", listOutput)
	}
}

func TestRun_PluginInstallRejectsMissingPackageArgument(t *testing.T) {
	t.Parallel()

	err := run([]string{"plugin", "install", "--config", "config.yaml"})
	if err == nil {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(err.Error(), "usage: gestaltd plugin install") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_PluginListRejectsTrailingArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"plugin", "list", "--config", "config.yaml", "extra"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := run(tt.args)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
			if !strings.Contains(err.Error(), "unexpected arguments") {
				t.Fatalf("expected trailing args error, got: %v", err)
			}
		})
	}
}

func TestRun_PluginRejectsUnknownSubcommand(t *testing.T) {
	t.Parallel()

	err := run([]string{"plugin", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown plugin subcommand")
	}
	if !strings.Contains(err.Error(), `unknown plugin command "bogus"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newPluginPackageFixture(t *testing.T, dir string) string {
	t.Helper()

	src := filepath.Join(dir, "src")
	artifactDir := filepath.Join(src, "artifacts", runtime.GOOS, runtime.GOARCH)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "provider"), []byte("provider"), 0755); err != nil {
		t.Fatalf("WriteFile(provider): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(src, "schemas"), 0755); err != nil {
		t.Fatalf("MkdirAll(schemas): %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "schemas", "config.schema.json"), []byte(`{"type":"object"}`), 0644); err != nil {
		t.Fatalf("WriteFile(schema): %v", err)
	}

	manifest := `{
  "schema_version": 1,
  "id": "acme/provider",
  "version": "0.1.0",
  "kinds": ["provider"],
  "provider": {
    "protocol": { "min": 1, "max": 1 },
    "config_schema_path": "schemas/config.schema.json"
  },
  "artifacts": [
    {
      "os": "` + runtime.GOOS + `",
      "arch": "` + runtime.GOARCH + `",
      "path": "artifacts/` + runtime.GOOS + `/` + runtime.GOARCH + `/provider",
      "sha256": "` + sha256HexForTest("provider") + `"
    }
  ],
  "entrypoints": {
    "provider": {
      "artifact_path": "artifacts/` + runtime.GOOS + `/` + runtime.GOARCH + `/provider"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile(plugin.json): %v", err)
	}
	return src
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()

	// This helper swaps the process-global os.Stdout, so callers must not run in
	// parallel with other tests in this package.
	stdoutMu.Lock()
	defer stdoutMu.Unlock()

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	runErr := fn()

	_ = w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	_ = r.Close()

	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	return buf.String()
}

func sha256HexForTest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
