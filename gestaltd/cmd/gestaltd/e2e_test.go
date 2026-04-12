package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/providerpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
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
	cfgBytes = append(cfgBytes, []byte(`  audit:
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
			auditYAML: `  audit:
    source: bogus
`,
			wantError: "unknown audit provider",
		},
		{
			name: "stdout audit requires mapping config",
			auditYAML: `  audit:
    source: stdout
    config: nope
`,
			wantError: "stdout audit: parsing config",
		},
		{
			name: "otlp audit rejects non-otlp logs exporter",
			auditYAML: `  audit:
    source: otlp
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
			pluginYAML: `source:
  path: /tmp/manifest.yaml
bogus: true`,
			wantError: "bogus",
		},
		{
			name: "removed plugin connection field",
			pluginYAML: `source:
  path: /tmp/manifest.yaml
connection: default`,
			wantError: "connection",
		},
		{
			name: "removed provider params field",
			pluginYAML: `source:
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
			cfg := "server:\n  encryptionKey: test-key\n" + authIndexedDBConfigYAML(t, dir, "local", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`  plugins:
    example:
      %s
`, strings.ReplaceAll(tc.pluginYAML, "\n", "\n      "))
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
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindPlugin,
		Source:      "github.com/test/plugins/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Spec:        &providermanifestv1.Spec{},
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
	if _, err := providerpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, providermanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindAuth,
		Source:      "github.com/test/providers/auth/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Auth " + name,
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
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

	manifestPath, err := providerpkg.FindManifestFile(providerDir)
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
		authBlock = fmt.Sprintf(`  auth:
    source:
      path: %s
`, authManifestPath)
	}
	return fmt.Sprintf(`  indexeddb: %s
providers:
%s  indexeddbs:
    %s:
      source:
        ref: github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb
        version: 0.0.1-alpha.2
      config:
        dsn: %q
  ui:
    disabled: true
`, datastoreName, authBlock, datastoreName, "sqlite://"+dbPath)
}

func writeManifestFile(t *testing.T, pluginDir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	data, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func reservePort(t *testing.T) (int, net.Listener) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	return l.Addr().(*net.TCPAddr).Port, l
}

func setupIndexedDBProviderDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "indexeddb-provider")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	binDest := filepath.Join(providerDir, filepath.Base(indexedDBBin))
	data, err := os.ReadFile(indexedDBBin)
	if err != nil {
		t.Fatalf("read indexeddb binary: %v", err)
	}
	if err := os.WriteFile(binDest, data, 0o755); err != nil {
		t.Fatalf("write indexeddb binary: %v", err)
	}

	artifactRel := filepath.Base(binDest)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIndexedDB,
		Source:      "github.com/test/providers/indexeddb-inmem",
		Version:     "0.0.1-alpha.1",
		DisplayName: "In-Memory IndexedDB",
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func setupPrebuiltPluginDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "plugin-prebuilt")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	binDest := filepath.Join(providerDir, "gestalt-plugin-example")
	binData, err := os.ReadFile(pluginBin)
	if err != nil {
		t.Fatalf("read plugin binary: %v", err)
	}
	if err := os.WriteFile(binDest, binData, 0o755); err != nil {
		t.Fatalf("write plugin binary: %v", err)
	}

	srcDir := testutil.MustExampleProviderPluginPath()
	catalogData, err := os.ReadFile(filepath.Join(srcDir, "catalog.yaml"))
	if err != nil {
		t.Fatalf("read catalog.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "catalog.yaml"), catalogData, 0o644); err != nil {
		t.Fatalf("write catalog.yaml: %v", err)
	}

	_, srcManifest, err := providerpkg.ReadSourceManifestFile(filepath.Join(srcDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}

	artifactRel := filepath.Base(binDest)
	srcManifest.Artifacts = []providermanifestv1.Artifact{
		{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
	}
	srcManifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactRel}
	writeManifestFile(t, providerDir, srcManifest)
	return providerDir
}

func writeServeConfig(t *testing.T, dir string, port int) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	pluginDir := setupPrebuiltPluginDir(t, dir)
	pluginManifest, err := providerpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-serve-e2e-key
  indexeddb: inmem
providers:
  ui:
    disabled: true
  indexeddbs:
    inmem:
      source:
        path: %s
  plugins:
    example:
      source:
        path: %s
`, port, indexedDBManifest, pluginManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func startGestaltd(t *testing.T, dir string) string {
	t.Helper()

	port, holder := reservePort(t)
	cfgPath := writeServeConfig(t, dir, port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	_ = holder.Close()
	cmd := exec.Command(gestaltdBin, "serve", "--config", cfgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	client := &http.Client{Timeout: 2 * time.Second}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	timeout := time.After(60 * time.Second)
	ready := false
	for !ready {
		select {
		case <-exited:
			t.Fatal("gestaltd exited before becoming ready")
		case <-timeout:
			t.Fatal("gestaltd did not become ready within 60 seconds")
		case <-tick.C:
			resp, err := client.Get(baseURL + "/ready")
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					ready = true
				}
			}
		}
	}

	return baseURL
}

func TestE2EServeAndHealthCheck(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E serve test in short mode")
	}

	dir := t.TempDir()
	baseURL := startGestaltd(t, dir)

	client := &http.Client{Timeout: 2 * time.Second}
	intResp, err := client.Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = intResp.Body.Close() }()
	body, _ := io.ReadAll(intResp.Body)
	if intResp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", intResp.StatusCode, body)
	}

	var integrations []json.RawMessage
	if err := json.Unmarshal(body, &integrations); err != nil {
		t.Fatalf("decode integrations response: %v (body: %s)", err, body)
	}
	if len(integrations) == 0 {
		t.Fatal("expected at least one integration from the example plugin")
	}
}

func TestE2EInitLocalProviders(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E init test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init failed: %v\noutput: %s", err, out)
	}

	lockPath := filepath.Join(dir, "gestalt.lock.json")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("expected lock file at %s: %v", lockPath, err)
	}

	var lock map[string]any
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatalf("invalid lock file JSON: %v", err)
	}
	version, _ := lock["version"].(float64)
	if version < 1 {
		t.Fatalf("expected lock version >= 1, got %v", lock["version"])
	}
}

func TestE2ECLIToServer(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E CLI-to-server test in short mode")
	}
	if gestaltCLIBin == "" {
		t.Skip("gestalt CLI binary not available (cargo not installed or build failed)")
	}

	dir := t.TempDir()
	baseURL := startGestaltd(t, dir)

	cliEnv := append(os.Environ(), "GESTALT_URL="+baseURL, "GESTALT_API_KEY=e2e-test-key")

	t.Run("integrations list", func(t *testing.T) {
		t.Parallel()
		cmd := exec.Command(gestaltCLIBin, "integrations", "list", "--format", "json", "--url", baseURL)
		cmd.Env = cliEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gestalt integrations list failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(string(out), "example") {
			t.Fatalf("expected 'example' integration in output, got: %s", out)
		}
	})

	t.Run("invoke echo operation", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command(gestaltCLIBin, "invoke", "example", "echo",
			"--format", "json",
			"--url", baseURL,
			"-p", "message=hello-e2e",
		)
		cmd.Env = cliEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gestalt invoke failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(string(out), "hello-e2e") {
			t.Fatalf("expected echo response to contain 'hello-e2e', got: %s", out)
		}
	})

	t.Run("describe operation", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command(gestaltCLIBin, "describe", "example", "echo",
			"--format", "json",
			"--url", baseURL,
		)
		cmd.Env = cliEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gestalt describe failed: %v\noutput: %s", err, out)
		}
		if !strings.Contains(string(out), "message") {
			t.Fatalf("expected 'message' parameter in describe output, got: %s", out)
		}
	})
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
	manifestPath, err := providerpkg.FindManifestFile(pluginDir)
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
	cfg := serverBlock + authIndexedDBConfigYAML(t, dir, "", "sqlite", dbPath) + fmt.Sprintf(`  plugins:
    example:
      source:
        path: %s
`, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
