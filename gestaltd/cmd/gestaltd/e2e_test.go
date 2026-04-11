package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestE2EValidateRejectsAuditConfigWhenProviderInheritsTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	cfgPath := writeE2EConfig(t, dir, pluginDir, 18080)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`audit:
  config:
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config audit: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
	}
	if !strings.Contains(string(out), "audit.config is not supported when audit.provider is") {
		t.Fatalf("expected inherit-provider audit config error, got: %s", out)
	}
}

func TestE2EValidateRejectsInvalidAuditSettings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		auditYAML string
		wantError string
	}{
		{
			name: "unknown audit provider",
			auditYAML: `audit:
  builtin: bogus
`,
			wantError: "unknown audit.provider",
		},
		{
			name: "stdout audit requires mapping config",
			auditYAML: `audit:
  builtin: stdout
  config: nope
`,
			wantError: "stdout audit: parsing config",
		},
		{
			name: "otlp audit rejects non-otlp logs exporter",
			auditYAML: `audit:
  builtin: otlp
  config:
    logs:
      exporter: stdout
`,
			wantError: "otlp audit: logs.exporter must be",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pluginDir := setupPluginDir(t, dir)

			cfgPath := writeE2EConfig(t, dir, pluginDir, 18080)
			cfgBytes, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			cfgBytes = append(cfgBytes, []byte(tc.auditYAML)...)
			if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
				t.Fatalf("write config audit: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected %q, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EValidateRejectsUnknownYAMLField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pluginYAML string
		wantError  string
	}{
		{
			name: "bogus field",
			pluginYAML: `provider:
  source:
    path: /tmp/manifest.yaml
bogus: true`,
			wantError: "bogus",
		},
		{
			name: "removed plugin connection field",
			pluginYAML: `provider:
  source:
    path: /tmp/manifest.yaml
connection: default`,
			wantError: "connection",
		},
		{
			name: "removed provider params field",
			pluginYAML: `provider:
  source:
    path: /tmp/manifest.yaml
params:
  tenant:
    required: true`,
			wantError: "params",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := authIndexedDBConfigYAML(t, dir, "local", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  encryptionKey: test-key
plugins:
  example:
    %s
`, strings.ReplaceAll(tc.pluginYAML, "\n", "\n    "))
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for unknown field, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) || !strings.Contains(string(out), "parsing config YAML") {
				t.Fatalf("expected output to mention %q and YAML parsing, got: %s", tc.wantError, out)
			}
		})

	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`{{{invalid yaml`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for malformed YAML, output: %s", out)
	}
	if !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention YAML parsing failure, got: %s", out)
	}
}

func setupPluginDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupPluginDirWithVersion(t, baseDir, "0.0.1-alpha.1")
}

func setupPluginDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, "plugin-src")
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	manifest := &pluginmanifestv1.Manifest{
		Source:      "github.com/test/plugins/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Plugin:      &pluginmanifestv1.Plugin{},
	}
	writeManifestFile(t, pluginDir, manifest)
	return pluginDir
}

func setupAuthProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "auth", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/auth/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "auth.go", []byte(authProviderSource(name)), 0o644)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth-provider"))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactPath), err)
	}
	if _, err := pluginpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, pluginmanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	writeManifestFile(t, providerDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/test/providers/auth/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Auth " + name,
		Auth:        &pluginmanifestv1.AuthMetadata{},
		Artifacts: []pluginmanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Auth: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactRel},
		},
	})
	return providerDir
}

func authProviderSource(name string) string {
	source := testutil.GeneratedAuthPackageSource()
	displayName := name
	if name != "" {
		displayName = strings.ToUpper(name[:1]) + name[1:]
	}
	source = strings.Replace(source, `Name:        "generated-auth"`, fmt.Sprintf(`Name:        %q`, name), 1)
	source = strings.Replace(source, `DisplayName: "Generated Auth"`, fmt.Sprintf(`DisplayName: %q`, displayName), 1)
	return source
}

func componentProviderManifestPath(t *testing.T, providerDir string) string {
	t.Helper()

	manifestPath, err := pluginpkg.FindManifestFile(providerDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", providerDir, err)
	}
	return manifestPath
}

func authIndexedDBConfigYAML(t *testing.T, dir, authName, datastoreName, dbPath string) string {
	t.Helper()

	authBlock := ""
	if authName != "" {
		authManifestPath := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, authName))
		authBlock = fmt.Sprintf(`auth:
  provider:
    source:
      path: %s
`, authManifestPath)
	}
	return fmt.Sprintf(`%sindexeddbs:
  %s:
    provider:
      source:
        ref: github.com/valon-technologies/gestalt-providers/datastore/relationaldb
        version: 0.0.1-alpha.1
    config:
      dsn: %q
indexeddb: %s
ui:
  disabled: true
`, authBlock, datastoreName, "sqlite://"+dbPath, datastoreName)
}

func writeManifestFile(t *testing.T, pluginDir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()
	data, err := pluginpkg.EncodeSourceManifestFormat(manifest, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeE2EConfig(t *testing.T, dir, pluginDir string, port int) string {
	t.Helper()
	return writeE2EConfigWithPaths(t, dir, pluginDir, filepath.Join(dir, "gestalt.db"), "", port)
}

func writeE2EConfigWithPaths(t *testing.T, dir, pluginDir, dbPath, artifactsDir string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	serverBlock := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
`, port)
	if artifactsDir != "" {
		serverBlock += fmt.Sprintf("  artifactsDir: %s\n", artifactsDir)
	}
	cfg := authIndexedDBConfigYAML(t, dir, "", "sqlite", dbPath) + fmt.Sprintf(`%splugins:
  example:
    provider:
      source:
        path: %s
`, serverBlock, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
