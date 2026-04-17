package main

import (
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
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
	cfgText := strings.Replace(string(cfgBytes), "plugins:\n", `  audit:
    primary:
      config:
        format: json
plugins:
`, 1)
	cfgBytes = []byte(cfgText)
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
    primary:
      source: bogus
`,
			wantError: "unknown audit provider",
		},
		{
			name: "stdout audit requires mapping config",
			auditYAML: `  audit:
    primary:
      source: stdout
      config: nope
`,
			wantError: "stdout audit: parsing config",
		},
		{
			name: "otlp audit rejects non-otlp logs exporter",
			auditYAML: `  audit:
    primary:
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
			cfgText := strings.Replace(string(cfgBytes), "plugins:\n", tc.auditYAML+"plugins:\n", 1)
			cfgBytes = []byte(cfgText)
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

func TestE2EValidateAcceptsV3TelemetryBuiltins(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		source         string
		telemetryBlock string
	}{
		{
			name:   "otlp",
			source: "otlp",
			telemetryBlock: `      config:
        endpoint: localhost:4317
        protocol: grpc
        insecure: true`,
		},
		{
			name:   "noop",
			source: "noop",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
			pluginManifest := componentProviderManifestPath(t, setupPrebuiltPluginDir(t, dir))

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := fmt.Sprintf(`apiVersion: %s
server:
  encryptionKey: valid-config-e2e-key
  providers:
    telemetry: primary
    indexeddb: inmem
providers:
  telemetry:
    primary:
      source: %s
%s
  indexeddb:
    inmem:
      source:
        path: %s
plugins:
  example:
    source:
      path: %s
`, config.APIVersionV3, tc.source, tc.telemetryBlock, indexedDBManifest, pluginManifest)
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err != nil {
				t.Fatalf("gestaltd validate failed: %v\noutput: %s", err, out)
			}
		})
	}
}

func TestE2EValidateRejectsInvalidConfigInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		cfg       string
		wantError string
	}{
		{
			name:      "malformed yaml",
			cfg:       "{{{invalid yaml",
			wantError: "parsing config YAML",
		},
		{
			name: "unknown field",
			cfg: `server:
  encryptionKey: test-key
  bogus: true
`,
			wantError: "bogus",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(tc.cfg), 0o644); err != nil {
				t.Fatalf("write invalid config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected output to mention %q, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EValidateConfigPathPrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		args   []string
		env    func(t *testing.T, root, home, workdir string) []string
		before func(t *testing.T, root, home, workdir string) []string
	}{
		{
			name: "flag overrides env cwd and default",
			args: []string{"validate"},
			before: func(t *testing.T, root, home, workdir string) []string {
				flagDir := filepath.Join(root, "flag")
				envDir := filepath.Join(root, "env")
				if _, err := os.Stat(writeValidValidateConfig(t, flagDir)); err != nil {
					t.Fatalf("valid flag config missing: %v", err)
				}
				writeInvalidValidateConfig(t, filepath.Join(envDir, "config.yaml"))
				writeInvalidValidateConfig(t, filepath.Join(workdir, "config.yaml"))
				writeInvalidValidateConfig(t, filepath.Join(home, ".gestaltd", "config.yaml"))
				return []string{"--config", filepath.Join(flagDir, "config.yaml")}
			},
			env: func(t *testing.T, root, home, workdir string) []string {
				t.Helper()
				return []string{"GESTALT_CONFIG=" + filepath.Join(root, "env", "config.yaml")}
			},
		},
		{
			name: "env overrides cwd and default",
			args: []string{"validate"},
			before: func(t *testing.T, root, home, workdir string) []string {
				envDir := filepath.Join(root, "env")
				if _, err := os.Stat(writeValidValidateConfig(t, envDir)); err != nil {
					t.Fatalf("valid env config missing: %v", err)
				}
				writeInvalidValidateConfig(t, filepath.Join(workdir, "config.yaml"))
				writeInvalidValidateConfig(t, filepath.Join(home, ".gestaltd", "config.yaml"))
				return nil
			},
			env: func(t *testing.T, root, home, workdir string) []string {
				t.Helper()
				return []string{"GESTALT_CONFIG=" + filepath.Join(root, "env", "config.yaml")}
			},
		},
		{
			name: "cwd config overrides default",
			args: []string{"validate"},
			before: func(t *testing.T, root, home, workdir string) []string {
				if _, err := os.Stat(writeValidValidateConfig(t, workdir)); err != nil {
					t.Fatalf("valid cwd config missing: %v", err)
				}
				writeInvalidValidateConfig(t, filepath.Join(home, ".gestaltd", "config.yaml"))
				return nil
			},
		},
		{
			name: "default local config used last",
			args: []string{"validate"},
			before: func(t *testing.T, root, home, workdir string) []string {
				defaultDir := filepath.Join(home, ".gestaltd")
				if _, err := os.Stat(writeValidValidateConfig(t, defaultDir)); err != nil {
					t.Fatalf("valid default config missing: %v", err)
				}
				return nil
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			home := filepath.Join(root, "home")
			workdir := filepath.Join(root, "work")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatalf("MkdirAll home: %v", err)
			}
			if err := os.MkdirAll(workdir, 0o755); err != nil {
				t.Fatalf("MkdirAll workdir: %v", err)
			}
			args := append([]string(nil), tc.args...)
			if tc.before != nil {
				args = append(args, tc.before(t, root, home, workdir)...)
			}

			cmd := exec.Command(gestaltdBin, args...)
			cmd.Dir = workdir
			cmd.Env = append(os.Environ(), "HOME="+home)
			if tc.env != nil {
				cmd.Env = append(cmd.Env, tc.env(t, root, home, workdir)...)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("gestaltd %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
			}
			if !strings.Contains(string(out), "config ok") {
				t.Fatalf("expected validate success, got: %s", out)
			}
		})
	}
}

func TestE2EValidateAcceptsLayeredConfigs(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	baseDir := filepath.Join(rootDir, "base")
	overrideDir := filepath.Join(rootDir, "overrides")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("MkdirAll base: %v", err)
	}
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll overrides: %v", err)
	}

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, rootDir))
	setupPluginDir(t, overrideDir)

	baseConfigPath := filepath.Join(baseDir, "base.yaml")
	baseConfig := fmt.Sprintf(`server:
  encryptionKey: test-key
  providers:
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
plugins:
  example:
    source:
      path: ./missing/manifest.yaml
`, indexedDBManifest, filepath.Join(rootDir, "gestalt.db"))
	if err := os.WriteFile(baseConfigPath, []byte(baseConfig), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	overrideConfigPath := filepath.Join(overrideDir, "local.yaml")
	overrideConfig := `plugins:
  example:
    source:
      path: ./plugin-src/manifest.yaml
`
	if err := os.WriteFile(overrideConfigPath, []byte(overrideConfig), 0o644); err != nil {
		t.Fatalf("WriteFile override config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", baseConfigPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected base config validate to fail, output: %s", out)
	}
	if !strings.Contains(string(out), "missing/manifest.yaml") {
		t.Fatalf("expected base config failure to mention missing manifest, got: %s", out)
	}

	out, err = exec.Command(gestaltdBin, "validate", "--config", baseConfigPath, "--config", overrideConfigPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected layered config validate to succeed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected layered config output to mention success, got: %s", out)
	}
}

func TestE2EValidateUsesScratchPreparedInstallsForLocalSourceConfigs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`server:
  encryptionKey: test-key
  providers:
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
plugins:
  example:
    source:
      path: %s
`, indexedDBManifest, filepath.Join(dir, "gestalt.db"), componentProviderManifestPath(t, pluginDir))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected validate to succeed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected validate output to mention success, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, operator.InitLockfileName)); !os.IsNotExist(err) {
		t.Fatalf("validate should not write lockfile, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("validate should not leave prepared artifacts in config dir, got err=%v", err)
	}
	overrideLockfilePath := filepath.Join(dir, "state", "validate", operator.InitLockfileName)
	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath, "--lockfile", overrideLockfilePath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected validate with --lockfile to succeed: %v\n%s", err, out)
	}
	if _, err := os.Stat(overrideLockfilePath); !os.IsNotExist(err) {
		t.Fatalf("validate should not write override lockfile, got err=%v", err)
	}

	providedArtifactsDir := filepath.Join(dir, "artifacts", "validate")
	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath, "--artifacts-dir", providedArtifactsDir).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate with --artifacts-dir to fail, got success:\n%s", out)
	}
	if _, err := os.Stat(providedArtifactsDir); !os.IsNotExist(err) {
		t.Fatalf("validate should not mutate provided artifacts dir, got err=%v", err)
	}
}

func TestE2EValidateRejectsInvalidPluginInvokesDependency(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerDir := setupPluginDirWithVersion(t, filepath.Join(dir, "caller"), "0.0.1-alpha.1")
	targetDir := setupPluginDirWithVersion(t, filepath.Join(dir, "target"), "0.0.1-alpha.1")
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`server:
  encryptionKey: test-key
  providers:
    indexeddb: sqlite
providers:
  indexeddb:
    sqlite:
      source:
        path: %s
      config:
        path: %q
plugins:
  caller:
    source:
      path: %s
    invokes:
      - plugin: target
        operation: missing
  target:
    source:
      path: %s
`, indexedDBManifest, filepath.Join(dir, "gestalt.db"), componentProviderManifestPath(t, callerDir), componentProviderManifestPath(t, targetDir))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail, got success:\n%s", out)
	}
	if !strings.Contains(string(out), `unknown effective operation`) {
		t.Fatalf("expected validate output to mention missing invokes operation, got: %s", out)
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

func setupCacheProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "cache", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/cache/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "cache.go", []byte(testutil.GeneratedCachePackageSource()), 0o644)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "cache-provider"))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactPath), err)
	}
	if _, err := providerpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, providermanifestv1.KindCache, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindCache,
		Source:      "github.com/test/providers/cache/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Cache " + name,
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
	indexedDBManifestPath := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	serverProvidersBlock := fmt.Sprintf(`  providers:
    indexeddb: %s
`, datastoreName)
	if authName != "" {
		authManifestPath := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, authName))
		serverProvidersBlock += fmt.Sprintf("    auth: %s\n", authName)
		authBlock = fmt.Sprintf(`  auth:
    %s:
      source:
        path: %s
`, authName, authManifestPath)
	}
	return fmt.Sprintf(`%s
providers:
%s  indexeddb:
    %s:
      source:
        path: %s
      config:
        dsn: %q
`, serverProvidersBlock, authBlock, datastoreName, indexedDBManifestPath, "sqlite://"+dbPath)
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
	sum := sha256.Sum256(binData)
	srcManifest.Source = "github.com/test/plugins/provider"
	srcManifest.Version = "0.0.1-alpha.1"
	srcManifest.Artifacts = []providermanifestv1.Artifact{
		{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel, SHA256: hex.EncodeToString(sum[:])},
	}
	srcManifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactRel}
	writeManifestFile(t, providerDir, srcManifest)
	return providerDir
}

type mountedUITestConfig struct {
	Name         string
	Path         string
	ManifestPath string
}

func setupMountedWebUIDir(t *testing.T, baseDir string) *mountedUITestConfig {
	t.Helper()
	return setupMountedWebUIDirWithRoutes(t, baseDir, nil)
}

func setupMountedWebUIDirWithRoutes(t *testing.T, baseDir string, routes []providermanifestv1.WebUIRoute) *mountedUITestConfig {
	t.Helper()

	uiDir := filepath.Join(baseDir, "mounted-webui")
	distDir := filepath.Join(uiDir, "dist")
	assetsDir := filepath.Join(distDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", assetsDir, err)
	}

	writeTestFile(t, uiDir, filepath.Join("dist", "index.html"), []byte(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Roadmap Review UI</title>
  </head>
  <body>
    <div id="app">Roadmap Review UI</div>
    <script type="module" src="assets/app.js"></script>
  </body>
</html>
`), 0o644)
	writeTestFile(t, uiDir, filepath.Join("dist", "assets", "app.js"), []byte(`window.__ROADMAP_REVIEW_UI__ = "ready";
`), 0o644)
	writeManifestFile(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindWebUI,
		Source:      "github.com/test/webui/roadmap-review",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Roadmap Review UI",
		Spec: &providermanifestv1.Spec{
			AssetRoot: "dist",
			Routes:    routes,
		},
	})

	return &mountedUITestConfig{
		Name:         "roadmap_review",
		Path:         "/create-customer-roadmap-review",
		ManifestPath: filepath.Join(uiDir, "manifest.yaml"),
	}
}

func setupDefaultLocalProvidersDir(t *testing.T, baseDir string) string {
	t.Helper()

	providersDir := filepath.Join(baseDir, "providers")
	indexedDBDir := filepath.Join(providersDir, "indexeddb", "relationaldb")
	if err := os.MkdirAll(indexedDBDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", indexedDBDir, err)
	}

	indexedDBBinDest := filepath.Join(indexedDBDir, filepath.Base(indexedDBBin))
	indexedDBData, err := os.ReadFile(indexedDBBin)
	if err != nil {
		t.Fatalf("read indexeddb binary: %v", err)
	}
	if err := os.WriteFile(indexedDBBinDest, indexedDBData, 0o755); err != nil {
		t.Fatalf("write indexeddb binary: %v", err)
	}
	writeManifestFile(t, indexedDBDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIndexedDB,
		Source:      "github.com/test/providers/indexeddb-relationaldb",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Relational IndexedDB",
		Spec:        &providermanifestv1.Spec{},
		Artifacts: []providermanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: filepath.Base(indexedDBBinDest)},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(indexedDBBinDest)},
	})

	rootUIDir := filepath.Join(providersDir, "web", "default")
	rootDistDir := filepath.Join(rootUIDir, "dist")
	if err := os.MkdirAll(rootDistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", rootDistDir, err)
	}
	writeTestFile(t, rootUIDir, filepath.Join("dist", "index.html"), []byte(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <title>Default Gestalt UI</title>
  </head>
  <body>
    <div id="app">Default Gestalt UI</div>
  </body>
</html>
`), 0o644)
	writeManifestFile(t, rootUIDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindWebUI,
		Source:      "github.com/test/webui/default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default Gestalt UI",
		Spec:        &providermanifestv1.Spec{AssetRoot: "dist"},
	})

	return providersDir
}

func writeServeConfig(t *testing.T, dir string, port int, mountedUI *mountedUITestConfig) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	pluginDir := setupPrebuiltPluginDir(t, dir)
	pluginManifest, err := providerpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}
	uiBlock := ""
	if mountedUI != nil {
		uiBlock = fmt.Sprintf(`  ui:
    %s:
      source:
        path: %q
      path: %s
`, mountedUI.Name, mountedUI.ManifestPath, mountedUI.Path)
	}

	cfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
%splugins:
  example:
    source:
      path: %s
`, port, indexedDBManifest, uiBlock, pluginManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeServeConfigWithManagement(t *testing.T, dir string, publicPort, managementPort int, mountedUI *mountedUITestConfig) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	pluginDir := setupPrebuiltPluginDir(t, dir)
	pluginManifest, err := providerpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}
	uiBlock := ""
	if mountedUI != nil {
		uiBlock = fmt.Sprintf(`  ui:
    %s:
      source:
        path: %q
      path: %s
`, mountedUI.Name, mountedUI.ManifestPath, mountedUI.Path)
	}

	cfg := fmt.Sprintf(`server:
  public:
    port: %d
  management:
    host: 127.0.0.1
    port: %d
  encryptionKey: test-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
%splugins:
  example:
    source:
      path: %s
`, publicPort, managementPort, indexedDBManifest, uiBlock, pluginManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func startGestaltdWithConfigsAndArgs(t *testing.T, cfgPaths []string, args []string, requiredPath string) string {
	t.Helper()
	if len(cfgPaths) == 0 {
		t.Fatal("startGestaltdWithConfigsAndArgs requires at least one config path")
	}

	port, holder := reservePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	primaryCfgPath := cfgPaths[0]
	cfgBytes, err := os.ReadFile(primaryCfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg := strings.Replace(string(cfgBytes), "port: 0", fmt.Sprintf("port: %d", port), 1)
	if err := os.WriteFile(primaryCfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_ = holder.Close()
	for _, cfgPath := range cfgPaths {
		args = append(args, "--config", cfgPath)
	}
	cmd := exec.Command(gestaltdBin, args...)
	if requiredPath != "" {
		startCommandAndWaitReadyAndFile(t, cmd, baseURL, requiredPath)
	} else {
		startCommandAndWaitReady(t, cmd, baseURL)
	}
	return baseURL
}

func startGestaltdWithConfigs(t *testing.T, cfgPaths []string, locked bool) string {
	t.Helper()
	args := []string{"serve"}
	if locked {
		args = append(args, "--locked")
	}
	return startGestaltdWithConfigsAndArgs(t, cfgPaths, args, "")
}

func startCommandAndWaitReady(t *testing.T, cmd *exec.Cmd, baseURL string) {
	t.Helper()

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
		if cmd.Process == nil {
			return
		}
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
	for {
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
					return
				}
			}
		}
	}
}

func startCommandAndWaitReadyAndFile(t *testing.T, cmd *exec.Cmd, baseURL, requiredPath string) {
	t.Helper()

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
		if cmd.Process == nil {
			return
		}
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
	for {
		select {
		case <-exited:
			t.Fatal("gestaltd exited before becoming ready")
		case <-timeout:
			t.Fatalf("gestaltd did not become ready and write %s within 60 seconds", requiredPath)
		case <-tick.C:
			if _, err := os.Stat(requiredPath); err != nil {
				continue
			}
			resp, err := client.Get(baseURL + "/ready")
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return
				}
			}
		}
	}
}

func startGestaltdWithConfig(t *testing.T, cfgPath string) string {
	t.Helper()
	return startGestaltdWithConfigs(t, []string{cfgPath}, false)
}

func startGestaltd(t *testing.T, dir string, mountedUI *mountedUITestConfig) string {
	t.Helper()
	return startGestaltdWithConfig(t, writeServeConfig(t, dir, 0, mountedUI))
}

func TestE2EServeAndHealthCheck(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E serve test in short mode")
	}

	dir := t.TempDir()
	baseURL := startGestaltd(t, dir, nil)

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

//nolint:paralleltest // Uses the default 8080 startup path intentionally.
func TestE2EDefaultServeAutoGeneratesLocalConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping default serve autogen test in short mode")
	}

	if l, err := net.Listen("tcp", "127.0.0.1:8080"); err != nil {
		t.Skipf("skipping default serve autogen test because 127.0.0.1:8080 is unavailable: %v", err)
	} else {
		_ = l.Close()
	}

	root := t.TempDir()
	home := filepath.Join(root, "home")
	workdir := filepath.Join(root, "work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll home: %v", err)
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll workdir: %v", err)
	}
	providersDir := setupDefaultLocalProvidersDir(t, root)

	cmd := exec.Command(gestaltdBin)
	cmd.Dir = workdir
	cmd.Env = []string{
		"HOME=" + home,
		"GESTALT_PROVIDERS_DIR=" + providersDir,
		"PATH=" + os.Getenv("PATH"),
	}
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cfgPath := filepath.Join(home, ".gestaltd", "config.yaml")
	startCommandAndWaitReadyAndFile(t, cmd, "http://127.0.0.1:8080", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", cfgPath, err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected generated config at %s: %v", cfgPath, err)
	}
	if cfg.Providers.UI["root"] == nil {
		t.Fatal(`Providers.UI["root"] = nil`)
	}
	if len(cfg.Plugins) != 0 {
		t.Fatalf("expected no default local plugins, got %#v", cfg.Plugins)
	}
}

func TestE2EServeSplitManagementRoutes(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping split management serve test in short mode")
	}

	dir := t.TempDir()
	mountedUI := setupMountedWebUIDir(t, dir)
	publicPort, publicHolder := reservePort(t)
	managementPort, managementHolder := reservePort(t)
	publicURL := fmt.Sprintf("http://127.0.0.1:%d", publicPort)
	managementURL := fmt.Sprintf("http://127.0.0.1:%d", managementPort)
	cfgPath := writeServeConfigWithManagement(t, dir, publicPort, managementPort, mountedUI)
	_ = publicHolder.Close()
	_ = managementHolder.Close()

	cmd := exec.Command(gestaltdBin, "serve", "--config", cfgPath)
	startCommandAndWaitReady(t, cmd, publicURL)

	client := &http.Client{Timeout: 2 * time.Second}
	for _, tc := range []struct {
		name         string
		url          string
		wantStatus   int
		wantContains string
	}{
		{
			name:       "public serves integrations API",
			url:        publicURL + "/api/v1/integrations",
			wantStatus: http.StatusOK,
		},
		{
			name:       "public hides metrics",
			url:        publicURL + "/metrics",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "public hides admin ui",
			url:        publicURL + "/admin/",
			wantStatus: http.StatusNotFound,
		},
		{
			name:         "public serves mounted ui",
			url:          publicURL + mountedUI.Path + "/sync",
			wantStatus:   http.StatusOK,
			wantContains: "Roadmap Review UI",
		},
		{
			name:       "management becomes ready",
			url:        managementURL + "/ready",
			wantStatus: http.StatusOK,
		},
		{
			name:       "management hides public api",
			url:        managementURL + "/api/v1/integrations",
			wantStatus: http.StatusNotFound,
		},
		{
			name:         "management serves metrics",
			url:          managementURL + "/metrics",
			wantStatus:   http.StatusOK,
			wantContains: "# TYPE",
		},
		{
			name:         "management serves admin ui",
			url:          managementURL + "/admin/",
			wantStatus:   http.StatusOK,
			wantContains: "Prometheus metrics",
		},
		{
			name:       "management hides mounted ui",
			url:        managementURL + mountedUI.Path + "/sync",
			wantStatus: http.StatusNotFound,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp, err := client.Get(tc.url)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.url, err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("expected %s %d, got %d: %s", tc.url, tc.wantStatus, resp.StatusCode, body)
			}
			if tc.wantContains != "" && !strings.Contains(string(body), tc.wantContains) {
				t.Fatalf("expected %s body to contain %q, got: %s", tc.url, tc.wantContains, body)
			}
		})
	}
}

func TestE2EServePluginOwnedUIWiring(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping plugin-owned mounted ui integrations test in short mode")
	}

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	pluginManifest := componentProviderManifestPath(t, setupPrebuiltPluginDir(t, dir))
	mountedUI := setupMountedWebUIDirWithRoutes(t, dir, []providermanifestv1.WebUIRoute{{
		Path:         "/*",
		AllowedRoles: []string{"viewer"},
	}})
	publicPort, publicHolder := reservePort(t)
	publicURL := fmt.Sprintf("http://127.0.0.1:%d", publicPort)
	cfgPath := filepath.Join(dir, "config-owned-ui.yaml")
	cfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-plugin-owned-ui-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
  ui:
    roadmap:
      source:
        path: %s
      path: /roadmap
plugins:
  example:
    source:
      path: %s
    ui: roadmap
    mountPath: /roadmap
`, publicPort, indexedDBManifest, mountedUI.ManifestPath, pluginManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write owned-ui config: %v", err)
	}

	loadedCfg, _, err := operatorLifecycle().LoadForExecutionAtPathsWithStatePaths([]string{cfgPath}, operator.StatePaths{}, false)
	if err != nil {
		t.Fatalf("LoadForExecutionAtPathsWithStatePaths(%s): %v", cfgPath, err)
	}
	if got := loadedCfg.Providers.UI["roadmap"].OwnerPlugin; got != "example" {
		t.Fatalf(`Providers.UI["roadmap"].OwnerPlugin = %q, want %q`, got, "example")
	}
	_ = publicHolder.Close()

	cmd := exec.Command(gestaltdBin, "serve", "--config", cfgPath)
	startCommandAndWaitReady(t, cmd, publicURL)

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(publicURL + "/roadmap/sync")
	if err != nil {
		t.Fatalf("GET /roadmap/sync: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /roadmap/sync 200, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "Roadmap Review UI") {
		t.Fatalf("expected mounted ui body to contain marker, got: %s", body)
	}

	integrationsResp, err := (&http.Client{Timeout: 2 * time.Second}).Get(publicURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = integrationsResp.Body.Close() }()
	integrationsBody, _ := io.ReadAll(integrationsResp.Body)
	if integrationsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", integrationsResp.StatusCode, integrationsBody)
	}
	var integrations []struct {
		Name        string `json:"name"`
		MountedPath string `json:"mountedPath"`
	}
	if err := json.Unmarshal(integrationsBody, &integrations); err != nil {
		t.Fatalf("json.Unmarshal integrations: %v (body: %s)", err, integrationsBody)
	}
	for _, integration := range integrations {
		if integration.Name == "example" && integration.MountedPath == "/roadmap" {
			return
		}
	}
	t.Fatalf(`integration "example" mountedPath missing from response: %s`, integrationsBody)
}

func TestE2EServeStartsWithPluginBoundCacheProvider(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E cache serve test in short mode")
	}

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	cacheManifest := componentProviderManifestPath(t, setupCacheProviderDir(t, dir, "session"))
	pluginManifest := componentProviderManifestPath(t, setupPrebuiltPluginDir(t, dir))
	cfgPath := filepath.Join(dir, "config-cache.yaml")

	cfg := fmt.Sprintf(`server:
  public:
    port: 0
  encryptionKey: test-cache-serve-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
  cache:
    session:
      source:
        path: %s
plugins:
  example:
    source:
      path: %s
    cache:
      - session
`, indexedDBManifest, cacheManifest, pluginManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	baseURL := startGestaltdWithConfig(t, cfgPath)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2EInitLocalProviders(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E init test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)

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
	if got, _ := lock["schema"].(string); got != "gestaltd-provider-lock" {
		t.Fatalf("expected provider lock schema, got %v", lock["schema"])
	}
	if got, _ := lock["schemaVersion"].(float64); got < 1 {
		t.Fatalf("expected schemaVersion >= 1, got %v", lock["schemaVersion"])
	}
	if _, ok := lock["version"]; ok {
		t.Fatalf("expected schema-based lockfile, found legacy version field: %v", lock["version"])
	}
}

func TestE2EInitWritesOverrideLockfile(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E init lockfile override test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "local", "gestalt.lock.json")

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init with --lockfile failed: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected override lockfile at %s: %v", lockPath, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func TestE2EInitAndServeLayeredConfigs(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping layered config E2E test in short mode")
	}

	dir := t.TempDir()
	basePath, overridePath, lockPath, _ := writeLayeredE2EConfigs(t, dir, 0)

	out, err := exec.Command(gestaltdBin, "init", "--config", basePath, "--config", overridePath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init with layered configs failed: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file at %s: %v", lockPath, err)
	}

	baseURL := startGestaltdWithConfigs(t, []string{basePath, overridePath}, true)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected /api/v1/integrations 401 with layered auth override, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2EServeAutoInitUsesOverrideLockfile(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E serve lockfile override test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "serve", "gestalt.lock.json")

	baseURL := startGestaltdWithConfigsAndArgs(t, []string{cfgPath}, []string{"serve", "--lockfile", lockPath}, lockPath)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func TestE2EDefaultServeAutoInitUsesOverrideLockfile(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping default serve lockfile override test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "default-serve", "gestalt.lock.json")

	baseURL := startGestaltdWithConfigsAndArgs(t, []string{cfgPath}, []string{"--lockfile", lockPath}, lockPath)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func TestE2EServeLockedUsesOverrideLockfile(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping locked serve lockfile override test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "locked-serve", "gestalt.lock.json")

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init with --lockfile failed: %v\noutput: %s", err, out)
	}
	baseURL := startGestaltdWithConfigsAndArgs(t, []string{cfgPath}, []string{"serve", "--locked", "--lockfile", lockPath}, "")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(baseURL + "/api/v1/integrations")
	if err != nil {
		t.Fatalf("GET /api/v1/integrations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected /api/v1/integrations 200, got %d: %s", resp.StatusCode, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
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
	baseURL := startGestaltd(t, dir, nil)

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

func writeValidValidateConfig(t *testing.T, dir string) string {
	t.Helper()

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	pluginManifest := componentProviderManifestPath(t, setupPrebuiltPluginDir(t, dir))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`server:
  encryptionKey: valid-config-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
plugins:
  example:
    source:
      path: %s
`, indexedDBManifest, pluginManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write valid config: %v", err)
	}
	return cfgPath
}

func writeInvalidValidateConfig(t *testing.T, path string) {
	t.Helper()

	dir := filepath.Dir(path)
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	cfg := fmt.Sprintf(`server:
  encryptionKey: invalid-config-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
plugins:
  example:
    source:
      path: %s
`, indexedDBManifest, filepath.Join(dir, "missing-plugin", "manifest.yaml"))
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
}

func writeLayeredE2EConfigs(t *testing.T, dir string, port int) (string, string, string, string) {
	t.Helper()

	deployDir := filepath.Join(dir, "deploy")
	overrideDir := filepath.Join(deployDir, "overrides")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", overrideDir, err)
	}

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	pluginManifest := componentProviderManifestPath(t, setupPrebuiltPluginDir(t, dir))
	authManifest := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, "local"))

	indexedDBRel, err := filepath.Rel(deployDir, indexedDBManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(indexeddb): %v", err)
	}
	pluginRel, err := filepath.Rel(deployDir, pluginManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(plugin): %v", err)
	}
	authRel, err := filepath.Rel(overrideDir, authManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(auth): %v", err)
	}

	basePath := filepath.Join(deployDir, "base.yaml")
	overridePath := filepath.Join(overrideDir, "local.yaml")
	baseCfg := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-layered-e2e-key
  providers:
    indexeddb: inmem
providers:
  indexeddb:
    inmem:
      source:
        path: %s
plugins:
  example:
    source:
      path: %s
`, port, filepath.ToSlash(indexedDBRel), filepath.ToSlash(pluginRel))
	overrideCfg := fmt.Sprintf(`server:
  providers:
    auth: local
  artifactsDir: ../artifacts/local
providers:
  auth:
    local:
      source:
        path: %s
`, filepath.ToSlash(authRel))

	if err := os.WriteFile(basePath, []byte(baseCfg), 0o644); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte(overrideCfg), 0o644); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	return basePath, overridePath, filepath.Join(deployDir, "gestalt.lock.json"), filepath.Join(deployDir, "artifacts", "local")
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
	cfg := serverBlock + authIndexedDBConfigYAML(t, dir, "", "sqlite", dbPath) + fmt.Sprintf(`plugins:
    example:
      source:
        path: %s
`, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
