package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

func TestE2EValidateRejectsAuditConfigWhenProviderInheritsTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, dir)

	cfgPath := writeE2EConfig(t, dir, appDir, 18080)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgText := strings.Replace(string(cfgBytes), "apps:\n", `  audit:
    primary:
      config:
        format: json
apps:
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
			appDir := setupAppDir(t, dir)

			cfgPath := writeE2EConfig(t, dir, appDir, 18080)
			cfgBytes, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			cfgText := strings.Replace(string(cfgBytes), "apps:\n", tc.auditYAML+"apps:\n", 1)
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

func TestE2EValidateAcceptsTelemetryBuiltins(t *testing.T) {
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
		{
			name:   "stdout",
			source: "stdout",
			telemetryBlock: `      config:
        level: debug
        format: json`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
			appManifest := componentProviderManifestPath(t, setupPrebuiltAppDir(t, dir))

			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := fmt.Sprintf(`apiVersion: %s
server:
  baseUrl: %s
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
apps:
  example:
    source:
      path: %s
`, config.ConfigAPIVersion, e2eLoopbackBaseURL(8080), tc.source, tc.telemetryBlock, indexedDBManifest, appManifest)
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

func TestE2EValidateAcceptsCanonicalConfigShapes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	appManifest := componentProviderManifestPath(t, setupPrebuiltAppDir(t, filepath.Join(dir, "app")))
	ui := setupMountedUIDir(t, dir)
	workflowManifest := componentProviderManifestPath(t, setupExecutableProviderDir(t, dir, providermanifestv1.KindWorkflow, "workflow-indexeddb"))
	agentManifest := componentProviderManifestPath(t, setupExecutableProviderDir(t, dir, providermanifestv1.KindAgent, "agent-simple"))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  encryptionKey: canonical-config-shapes-key
  providers:
    indexeddb: inmem
  runtime:
    defaultProvider: hosted
providers:
  indexeddb:
    inmem:
      source:
        path: %s
  ui:
    roadmap:
      source:
        path: %s
  workflow:
    local:
      source:
        path: %s
      default: true
      indexeddb:
        provider: inmem
        db: workflow_state
  agent:
    simple:
      source:
        path: %s
      default: true
      indexeddb:
        provider: inmem
        db: agent_state
      runtime:
        provider: hosted
        image: ghcr.io/example/agent:latest
        pool:
          minReadyInstances: 1
          maxReadyInstances: 2
          startupTimeout: 5m
          healthCheckInterval: 30s
          restartPolicy: always
          drainTimeout: 2m
runtime:
  providers:
    hosted:
      driver: local
apps:
  roadmap:
    source:
      path: %s
    ui:
      path: /roadmap
      bundle: roadmap
workflows:
  definitions:
    nightly_sync:
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: sync
          app:
            name: roadmap
            operation: sync
            connection: default
            instance: tenant-a
            input:
              mode: incremental
      on:
        nightly:
          schedule:
            cron: "0 2 * * *"
    nightly_summary:
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: summarize
          timeout: 120s
          agent:
            provider: simple
            model: fast
            prompt: Summarize yesterday.
            output:
              text: {}
            tools:
              - app: roadmap
                operation: sync
      on:
        nightly:
          schedule:
            cron: "0 3 * * *"
    roadmap_updated:
      runAs:
        subject:
          id: service_account:roadmap-workflow
      steps:
        - id: sync
          app:
            name: roadmap
            operation: sync
            input:
              mode: event
      on:
        roadmap_updated:
          event:
            type: roadmap.item.updated
            source: roadmap
`, e2eLoopbackBaseURL(8080), indexedDBManifest, ui.ManifestPath, workflowManifest, agentManifest, appManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected config ok, got: %s", out)
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
			name: "missing apiVersion",
			cfg: `server:
  encryptionKey: test-key
`,
			wantError: "apiVersion is required",
		},
		{
			name: "empty apiVersion",
			cfg: `apiVersion: ""
server:
  encryptionKey: test-key
`,
			wantError: "apiVersion is required",
		},
		{
			name: "unknown field",
			cfg: `apiVersion: gestaltd.config/v7
server:
  encryptionKey: test-key
  bogus: true
`,
			wantError: "bogus",
		},
		{
			name: "ui object requires path",
			cfg: `apiVersion: gestaltd.config/v7
apps:
  roadmap:
    source:
      path: ./app/manifest.yaml
    ui:
      bundle: roadmap
`,
			wantError: "ui.path is required when ui is an object",
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

func TestE2EProviderValidateIsolatedSourceApp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	providersDir := setupDefaultLocalProvidersDir(t, dir)
	appDir := setupAppDir(t, dir)

	cmd := exec.Command(gestaltdBin, "provider", "validate", "--path", appDir)
	cmd.Env = append(os.Environ(),
		"GESTALT_PROVIDERS_DIR="+providersDir,
		"GOTELEMETRY=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd provider validate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected validate success, got: %s", out)
	}
	if !strings.Contains(string(out), "provider validated") {
		t.Fatalf("expected provider validation summary, got: %s", out)
	}
}

func TestE2EProviderValidateSourceUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	providersDir := setupDefaultLocalProvidersDir(t, dir)
	mountedUI := setupMountedUIDir(t, dir)
	setUIManifestSource(t, mountedUI.ManifestPath, "github.com/test/ui/roadmap.review")

	cmd := exec.Command(gestaltdBin, "provider", "validate", "--path", mountedUI.ManifestPath)
	cmd.Env = append(os.Environ(),
		"GESTALT_PROVIDERS_DIR="+providersDir,
		"GOTELEMETRY=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd provider validate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected validate success, got: %s", out)
	}
	if !strings.Contains(string(out), "ui=roadmap_review") {
		t.Fatalf("expected ui validation summary, got: %s", out)
	}
	if !strings.Contains(string(out), "mounted_ui_paths=[/roadmap.review]") {
		t.Fatalf("expected source-slug ui mount path in output, got: %s", out)
	}
}

func TestE2EProviderValidateReusesConfiguredAppKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	providersDir := setupDefaultLocalProvidersDir(t, dir)
	appDir := setupAppDir(t, dir)
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `apiVersion: gestaltd.config/v7
apps:
  provider_go:
    source: https://example.test/provider-release.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(gestaltdBin, "provider", "validate", "--path", appDir, "--config", cfgPath, "--name", "provider_go")
	cmd.Env = append(os.Environ(),
		"GESTALT_PROVIDERS_DIR="+providersDir,
		"GOTELEMETRY=off",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd provider validate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "app=provider_go") {
		t.Fatalf("expected configured app key in output, got: %s", out)
	}
}

func TestE2EProviderValidateRejectsNonAppManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))

	out, err := exec.Command(gestaltdBin, "provider", "validate", "--path", manifestPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected gestaltd provider validate to fail for non-app manifest\n%s", out)
	}
	if !strings.Contains(string(out), "only support kind: app or ui in v1") {
		t.Fatalf("expected app-only error, got: %s", out)
	}
}

func TestE2EProviderValidateLayeredConfigSupportsNullDeletion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	providersDir := setupDefaultLocalProvidersDir(t, dir)
	targetDir := setupAppDir(t, filepath.Join(dir, "target"))
	supportDir := setupPrebuiltAppDir(t, filepath.Join(dir, "support"))
	mountedUI := setupMountedUIDir(t, dir)
	attachOwnedUIToAppSource(t, targetDir, mountedUI.ManifestPath)

	supportManifest := componentProviderManifestPath(t, supportDir)
	supportRel, err := filepath.Rel(dir, supportManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(support): %v", err)
	}
	uiRel, err := filepath.Rel(dir, mountedUI.ManifestPath)
	if err != nil {
		t.Fatalf("filepath.Rel(ui): %v", err)
	}

	baseCfgPath := filepath.Join(dir, "support.yaml")
	baseCfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
providers:
  ui:
    roadmap:
      source:
        path: %s
      path: /provider
apps:
  support:
    source:
      path: %s
    ui:
      bundle: roadmap
      path: /provider
`, filepath.ToSlash(uiRel), filepath.ToSlash(supportRel))
	if err := os.WriteFile(baseCfgPath, []byte(baseCfg), 0o644); err != nil {
		t.Fatalf("write support config: %v", err)
	}

	overrideCfgPath := filepath.Join(dir, "support-override.yaml")
	overrideCfg := `apiVersion: gestaltd.config/v7
providers:
  ui:
    roadmap: null
apps:
  support: null
`
	if err := os.WriteFile(overrideCfgPath, []byte(overrideCfg), 0o644); err != nil {
		t.Fatalf("write support override: %v", err)
	}

	failingCmd := exec.Command(gestaltdBin, "provider", "validate", "--path", targetDir, "--config", baseCfgPath)
	failingCmd.Env = append(os.Environ(),
		"GESTALT_PROVIDERS_DIR="+providersDir,
		"GOTELEMETRY=off",
	)
	failingOut, err := failingCmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected provider validate without null deletion to fail\n%s", failingOut)
	}
	if !strings.Contains(string(failingOut), `collides with providers.ui.roadmap`) {
		t.Fatalf("expected mount collision error, got: %s", failingOut)
	}

	successCmd := exec.Command(gestaltdBin, "provider", "validate", "--path", targetDir, "--config", baseCfgPath, "--config", overrideCfgPath)
	successCmd.Env = append(os.Environ(),
		"GESTALT_PROVIDERS_DIR="+providersDir,
		"GOTELEMETRY=off",
	)
	successOut, err := successCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd provider validate with null deletion failed: %v\noutput: %s", err, successOut)
	}
	if !strings.Contains(string(successOut), "config ok") {
		t.Fatalf("expected validate success with null deletion, got: %s", successOut)
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
			cmd.Env = append(os.Environ(),
				"HOME="+home,
				"GOTELEMETRY=off",
			)
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
	setupAppDir(t, overrideDir)

	baseConfigPath := filepath.Join(baseDir, "base.yaml")
	baseConfig := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
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
apps:
  example:
    source:
      path: ./missing/manifest.yaml
`, e2eLoopbackBaseURL(8080), indexedDBManifest, filepath.Join(rootDir, "gestalt.db"))
	if err := os.WriteFile(baseConfigPath, []byte(baseConfig), 0o644); err != nil {
		t.Fatalf("WriteFile base config: %v", err)
	}

	overrideConfigPath := filepath.Join(overrideDir, "local.yaml")
	overrideConfig := `apiVersion: gestaltd.config/v7
apps:
  example:
    source:
      path: ./app-src/manifest.yaml
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
	appDir := setupAppDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
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
apps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(8080), indexedDBManifest, filepath.Join(dir, "gestalt.db"), componentProviderManifestPath(t, appDir))
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
	if _, err := os.Stat(filepath.Join(dir, operator.LockfileName)); !os.IsNotExist(err) {
		t.Fatalf("validate should not write lockfile, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("validate should not leave prepared artifacts in config dir, got err=%v", err)
	}
	overrideLockfilePath := filepath.Join(dir, "state", "validate", operator.LockfileName)
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

func TestE2EValidateStaticAcceptsMissingRuntimeEnvPlaceholder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeValidValidateConfig(t, dir)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	data = []byte(strings.Replace(string(data), "encryptionKey: valid-config-e2e-key", "encryptionKey: ${GESTALT_E2E_VALIDATE_MISSING_KEY}", 1))
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected static validate to succeed with missing runtime env placeholder: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "config ok") {
		t.Fatalf("expected validate success, got: %s", out)
	}
}

func TestE2EValidatePlatformUsesLockedStaticMetadataWithoutArchiveDownload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	appDir := setupPrebuiltAppDir(t, dir)
	if err := writeLocalProviderReleaseMetadata(appDir); err != nil {
		t.Fatalf("write app provider-release metadata: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  encryptionKey: valid-config-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  example:
    source: %s
`, e2eLoopbackBaseURL(8080), externalCredentialsManifest, indexedDBManifest, filepath.Join(appDir, "provider-release.yaml"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	lockPath := filepath.Join(dir, "gestalt.lock.json")
	out, err := exec.Command(gestaltdBin, "lock", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock failed: %v\n%s", err, out)
	}

	var archiveHits atomic.Int64
	archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		archiveHits.Add(1)
		http.Error(w, "static validate must not fetch this archive", http.StatusTeapot)
	}))
	t.Cleanup(archiveServer.Close)

	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	var lock map[string]any
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}
	providers := lock["providers"].(map[string]any)
	apps := providers["app"].(map[string]any)
	example := apps["example"].(map[string]any)
	example["archives"] = map[string]any{
		"linux/amd64": map[string]any{
			"url":    archiveServer.URL + "/provider.tar.gz",
			"sha256": strings.Repeat("a", 64),
		},
	}
	updated, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatalf("marshal lockfile: %v", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(lockPath, updated, 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	out, err = exec.Command(gestaltdBin, "validate", "--platform", "linux/amd64", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected platform validate to use locked static metadata: %v\n%s", err, out)
	}
	if got := archiveHits.Load(); got != 0 {
		t.Fatalf("archive fetches = %d, want 0", got)
	}
}

func TestE2EValidateStaticRejectsStaleCatalogExposureMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	callerManifest := componentProviderManifestPath(t, setupPrebuiltAppDir(t, filepath.Join(dir, "caller")))
	targetDir := setupPrebuiltAppDir(t, filepath.Join(dir, "target"))
	if err := writeLocalProviderReleaseMetadata(targetDir); err != nil {
		t.Fatalf("write target provider-release metadata: %v", err)
	}
	targetReleaseMetadata := filepath.Join(targetDir, "provider-release.yaml")
	lockPath := filepath.Join(dir, "gestalt.lock.json")
	cfgPath := filepath.Join(dir, "config.yaml")
	writeConfig := func(targetOperation string) {
		t.Helper()
		cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  encryptionKey: valid-config-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  caller:
    source:
      path: %s
  target:
    source: %s
    allowedOperations:
      %s: {}
`, e2eLoopbackBaseURL(8080), externalCredentialsManifest, indexedDBManifest, callerManifest, targetReleaseMetadata, targetOperation)
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	writeConfig("echo")
	out, err := exec.Command(gestaltdBin, "lock", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock failed: %v\n%s", err, out)
	}

	writeConfig("greet")
	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected stale static catalog metadata to fail validation:\n%s", out)
	}
	if !strings.Contains(string(out), `lock entry for app \"target\" is stale`) {
		t.Fatalf("expected stale target lock error, got: %s", out)
	}
}

func TestE2EValidateStaticPlatformRejectsExplicitMissingLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  encryptionKey: valid-config-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  remote:
    source: https://example.com/provider-release.yaml
`, e2eLoopbackBaseURL(8080), externalCredentialsManifest, indexedDBManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	lockPath := filepath.Join(dir, "missing.lock.json")
	out, err := exec.Command(gestaltdBin, "validate", "--platform", runtime.GOOS+"/"+runtime.GOARCH, "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected explicit missing lockfile to fail:\n%s", out)
	}
	if !strings.Contains(string(out), "lockfile is missing or unreadable") {
		t.Fatalf("expected missing lockfile error, got: %s", out)
	}
}

func TestE2EValidateRejectsAppInvokesField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	callerDir := setupAppDirWithVersion(t, filepath.Join(dir, "caller"), "0.0.1-alpha.1")
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
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
apps:
  caller:
    source:
      path: %s
    invokes:
      - app: target
        operation: missing
`, e2eLoopbackBaseURL(8080), indexedDBManifest, filepath.Join(dir, "gestalt.db"), componentProviderManifestPath(t, callerDir))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail, got success:\n%s", out)
	}
	if !strings.Contains(string(out), `field invokes not found`) {
		t.Fatalf("expected validate output to reject invokes field, got: %s", out)
	}
}

func TestE2EValidateRejectsHybridExecutableDuplicateEffectiveOperation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	appDir := setupAppDir(t, filepath.Join(dir, "target"))
	manifestPath := componentProviderManifestPath(t, appDir)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("read source manifest: %v", err)
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	manifest.Spec.Surfaces = &providermanifestv1.ProviderSurfaces{
		OpenAPI: &providermanifestv1.OpenAPISurface{Document: "openapi.yaml"},
	}
	manifest.Spec.AllowedOperations = map[string]*providermanifestv1.ManifestOperationOverride{
		"external_echo": {Alias: "echo"},
	}
	writeManifestFile(t, appDir, manifest)
	if err := os.WriteFile(filepath.Join(appDir, "openapi.yaml"), []byte(`openapi: "3.1.0"
info:
  title: Hybrid Duplicate
  version: "1.0.0"
paths:
  /external-echo:
    get:
      operationId: external_echo
      responses:
        "200":
          description: OK
`), 0o644); err != nil {
		t.Fatalf("write OpenAPI document: %v", err)
	}

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
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
apps:
  target:
    source:
      path: %s
`, e2eLoopbackBaseURL(8080), indexedDBManifest, filepath.Join(dir, "gestalt.db"), manifestPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail, got success:\n%s", out)
	}
	if !strings.Contains(string(out), `duplicate operation \"echo\" across merged catalogs`) {
		t.Fatalf("expected validate output to mention duplicate effective operation, got: %s", out)
	}
}

func setupAppDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupAppDirWithVersion(t, baseDir, "0.0.1-alpha.1")
}

func setupAppDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	appDir := filepath.Join(baseDir, "app-src")
	testutil.CopyExampleProviderApp(t, appDir)
	artifactRel := ".gestalt/build/provider"
	writeGoAppBuildFixture(t, appDir, "github.com/valon-technologies/gestalt/testdata/provider-go", "example", artifactRel)
	manifest := &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindApp,
		Source:      "github.com/test/apps/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "provider.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	}
	writeManifestFile(t, appDir, manifest)
	return appDir
}

func setAppManifestSource(t *testing.T, appDir, source string) {
	t.Helper()

	manifestPath := componentProviderManifestPath(t, appDir)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", manifestPath, err)
	}
	manifest.Source = source
	writeManifestFile(t, appDir, manifest)
}

func setUIManifestSource(t *testing.T, manifestPath, source string) {
	t.Helper()

	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", manifestPath, err)
	}
	manifest.Source = source
	writeManifestFile(t, filepath.Dir(manifestPath), manifest)
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
	artifactRel := ".gestalt/build/auth-provider"
	writeGoComponentBuildFixture(t, providerDir, "example.com/providers/auth/"+name, providermanifestv1.KindIdentity, artifactRel)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindIdentity,
		Source:      "github.com/test/providers/auth/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Auth " + name,
		Spec:        &providermanifestv1.Spec{},
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "auth.go", "cmd", "build.sh"},
		},
		Entrypoint: &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func setupExecutableProviderDir(t *testing.T, baseDir, kind, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, kind, name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "gestalt-"+name))
	binDest := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(binDest), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(binDest), err)
	}

	switch kind {
	case providermanifestv1.KindWorkflow:
		writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/workflow/"+name)), 0o644)
		writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
		writeTestFile(t, providerDir, "workflow.go", []byte(testutil.GeneratedWorkflowPackageSource()), 0o644)
		artifactRel = ".gestalt/build/workflow-provider"
		writeGoComponentBuildFixture(t, providerDir, "example.com/providers/workflow/"+name, providermanifestv1.KindWorkflow, artifactRel)
	case providermanifestv1.KindAgent:
		if err := testutil.BuildSDKTestMainBinary(testutil.MustSDKTestProviderPath("agent"), binDest); err != nil {
			t.Fatalf("build agent provider fixture: %v", err)
		}
	default:
		binData, err := os.ReadFile(appBin)
		if err != nil {
			t.Fatalf("read provider binary: %v", err)
		}
		if err := os.WriteFile(binDest, binData, 0o755); err != nil {
			t.Fatalf("write provider binary: %v", err)
		}
	}
	manifest := &providermanifestv1.Manifest{
		Kind:        kind,
		Source:      "github.com/test/providers/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: name,
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	}
	if kind == providermanifestv1.KindWorkflow {
		manifest.Build = &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"go.mod", "go.sum", "workflow.go", "cmd", "build.sh"},
		}
	}
	writeManifestFile(t, providerDir, manifest)
	return providerDir
}

func writeGoComponentBuildFixture(t *testing.T, providerDir, importPath, kind, artifactRel string) {
	t.Helper()

	serveCall := goComponentServeCallForTest(t, kind)
	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := %s; err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, serveCall)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func writeGoAppBuildFixture(t *testing.T, providerDir, importPath, appName, artifactRel string) {
	t.Helper()

	mainSource := fmt.Sprintf(`package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	providerpkg %q
	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := gestalt.ServeProvider(ctx, providerpkg.New(), providerpkg.Router.WithName(%q)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %%v\n", err)
		os.Exit(1)
	}
}
`, importPath, appName)
	writeTestFile(t, providerDir, filepath.Join("cmd", "provider", "main.go"), []byte(mainSource), 0o644)
	buildScript := fmt.Sprintf("mkdir -p %q\ngo build -o %q ./cmd/provider\n", filepath.ToSlash(filepath.Dir(artifactRel)), artifactRel)
	writeTestFile(t, providerDir, "build.sh", []byte(buildScript), 0o755)
}

func goComponentServeCallForTest(t *testing.T, kind string) string {
	t.Helper()
	switch providermanifestv1.NormalizeKind(kind) {
	case providermanifestv1.KindIdentity:
		return "gestalt.ServeIdentityProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindAuthorization:
		return "gestalt.ServeAuthorizationProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindCache:
		return "gestalt.ServeCacheProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindWorkflow:
		return "gestalt.ServeWorkflowProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindExternalCredentials:
		return "gestalt.ServeExternalCredentialProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindSecrets:
		return "gestalt.ServeSecretsProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindIndexedDB:
		return "gestalt.ServeIndexedDBProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindS3:
		return "gestalt.ServeS3Provider(ctx, providerpkg.New())"
	case providermanifestv1.KindAgent:
		return "gestalt.ServeAgentProvider(ctx, providerpkg.New())"
	case providermanifestv1.KindRuntime:
		return "gestalt.ServeRuntimeProvider(ctx, providerpkg.New())"
	default:
		t.Fatalf("unsupported Go component fixture kind %q", kind)
		return ""
	}
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

func authIndexedDBConfigYAML(t *testing.T, dir, authName, indexedDBName, dbPath string) string {
	t.Helper()

	identityBlock := ""
	indexedDBManifestPath := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifestPath := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	const externalCredentialsName = "default"
	serverProvidersBlock := fmt.Sprintf(`  providers:
    indexeddb: %s
    externalCredentials: %s
`, indexedDBName, externalCredentialsName)
	if authName != "" {
		authManifestPath := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, authName))
		serverProvidersBlock += fmt.Sprintf("    identity: %s\n", authName)
		identityBlock = fmt.Sprintf(`  identity:
    %s:
      source:
        path: %s
`, authName, authManifestPath)
	}
	return fmt.Sprintf(`%s
providers:
%s  externalCredentials:
    %s:
      source:
        path: %s
  indexeddb:
    %s:
      source:
        path: %s
      config:
        dsn: %q
`, serverProvidersBlock, identityBlock, externalCredentialsName, externalCredentialsManifestPath, indexedDBName, indexedDBManifestPath, "sqlite://"+dbPath)
}

func writeManifestFile(t *testing.T, appDir string, manifest *providermanifestv1.Manifest) {
	t.Helper()
	data, err := providerpkg.EncodeSourceManifestFormat(manifest, providerpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// e2ePortBindMu serializes releasing reserved ports and starting gestaltd so parallel
// serve tests cannot steal each other's ports between listener close and process bind.
var e2ePortBindMu sync.Mutex

func reservePort(t *testing.T) (int, net.Listener) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	return l.Addr().(*net.TCPAddr).Port, l
}

func releasePortHoldersAndStart(t *testing.T, holders []net.Listener, start func()) {
	t.Helper()
	e2ePortBindMu.Lock()
	defer e2ePortBindMu.Unlock()
	for _, holder := range holders {
		if holder != nil {
			_ = holder.Close()
		}
	}
	start()
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
		Source:      "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Relational IndexedDB",
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func setupExternalCredentialsProviderDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "external-credentials-provider")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	binDest := filepath.Join(providerDir, filepath.Base(externalCredentialsBin))
	data, err := os.ReadFile(externalCredentialsBin)
	if err != nil {
		t.Fatalf("read external credentials binary: %v", err)
	}
	if err := os.WriteFile(binDest, data, 0o755); err != nil {
		t.Fatalf("write external credentials binary: %v", err)
	}

	artifactRel := filepath.Base(binDest)
	writeManifestFile(t, providerDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindExternalCredentials,
		Source:      "github.com/test/providers/external-credentials-default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default External Credentials",
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: artifactRel},
	})
	return providerDir
}

func setupPrebuiltAppDir(t *testing.T, baseDir string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "app-prebuilt")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}

	binDest := filepath.Join(providerDir, "gestalt-app-example")
	binData, err := os.ReadFile(appBin)
	if err != nil {
		t.Fatalf("read app binary: %v", err)
	}
	if err := os.WriteFile(binDest, binData, 0o755); err != nil {
		t.Fatalf("write app binary: %v", err)
	}

	srcDir := testutil.MustExampleProviderAppPath()
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
	srcManifest.Source = "github.com/test/apps/provider"
	srcManifest.Version = "0.0.1-alpha.1"
	srcManifest.Build = nil
	srcManifest.Artifacts = nil
	srcManifest.Entrypoint = &providermanifestv1.Entrypoint{ArtifactPath: artifactRel}
	writeManifestFile(t, providerDir, srcManifest)
	return providerDir
}

type mountedUITestConfig struct {
	Name         string
	Path         string
	ManifestPath string
}

func setupMountedUIDir(t *testing.T, baseDir string) *mountedUITestConfig {
	t.Helper()
	return setupMountedUIDirWithRoutes(t, baseDir, nil)
}

func attachOwnedUIToAppSource(t *testing.T, appDir, uiManifestPath string) {
	t.Helper()

	manifestPath := componentProviderManifestPath(t, appDir)
	_, manifest, err := providerpkg.ReadSourceManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadSourceManifestFile(%s): %v", manifestPath, err)
	}
	if manifest.Spec == nil {
		manifest.Spec = &providermanifestv1.Spec{}
	}
	relativeUIPath, err := filepath.Rel(appDir, uiManifestPath)
	if err != nil {
		t.Fatalf("filepath.Rel(%s, %s): %v", appDir, uiManifestPath, err)
	}
	manifest.Spec.UI = &providermanifestv1.OwnedUI{Path: filepath.ToSlash(relativeUIPath)}
	writeManifestFile(t, appDir, manifest)
}

func setupMountedUIDirWithRoutes(t *testing.T, baseDir string, routes []providermanifestv1.UIRoute) *mountedUITestConfig {
	t.Helper()

	return setupMountedUIDirAt(t, filepath.Join(baseDir, "mounted-ui"), routes)
}

func setupMountedUIDirAt(t *testing.T, uiDir string, routes []providermanifestv1.UIRoute) *mountedUITestConfig {
	t.Helper()

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
	writeTestFile(t, uiDir, "build.sh", []byte("mkdir -p dist/assets\nprintf '<html>Roadmap Review UI</html>\\n' > dist/index.html\nprintf 'window.__ROADMAP_REVIEW_UI__ = \"ready\";\\n' > dist/assets/app.js\n"), 0o755)
	writeManifestFile(t, uiDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/test/ui/roadmap-review",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Roadmap Review UI",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
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
		Source:      "github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Relational IndexedDB",
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(indexedDBBinDest)},
	})

	externalCredentialsDir := filepath.Join(providersDir, "externalcredentials", "default")
	if err := os.MkdirAll(externalCredentialsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", externalCredentialsDir, err)
	}
	externalCredentialsBinDest := filepath.Join(externalCredentialsDir, filepath.Base(externalCredentialsBin))
	externalCredentialsData, err := os.ReadFile(externalCredentialsBin)
	if err != nil {
		t.Fatalf("read external credentials binary: %v", err)
	}
	if err := os.WriteFile(externalCredentialsBinDest, externalCredentialsData, 0o755); err != nil {
		t.Fatalf("write external credentials binary: %v", err)
	}
	writeManifestFile(t, externalCredentialsDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindExternalCredentials,
		Source:      "github.com/test/providers/external-credentials-default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default External Credentials",
		Spec:        &providermanifestv1.Spec{},
		Entrypoint:  &providermanifestv1.Entrypoint{ArtifactPath: filepath.Base(externalCredentialsBinDest)},
	})

	rootUIDir := filepath.Join(providersDir, "ui", "default")
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
	writeTestFile(t, rootUIDir, "build.sh", []byte("mkdir -p dist\nprintf '<html>Default Gestalt UI</html>\\n' > dist/index.html\n"), 0o755)
	writeManifestFile(t, rootUIDir, &providermanifestv1.Manifest{
		Kind:        providermanifestv1.KindUI,
		Source:      "github.com/test/ui/default",
		Version:     "0.0.1-alpha.1",
		DisplayName: "Default Gestalt UI",
		Build: &providermanifestv1.SourceBuild{
			Command: []string{"sh", "./build.sh"},
			Inputs:  []string{"build.sh"},
		},
		Spec: &providermanifestv1.Spec{AssetRoot: "dist"},
	})

	return providersDir
}

func writeServeConfig(t *testing.T, dir string, port int, mountedUI *mountedUITestConfig) string {
	t.Helper()

	indexedDBDir := setupIndexedDBProviderDir(t, dir)
	indexedDBManifest := componentProviderManifestPath(t, indexedDBDir)
	appDir := setupPrebuiltAppDir(t, dir)
	appManifest, err := providerpkg.FindManifestFile(appDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", appDir, err)
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

	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
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
%sapps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(port), port, indexedDBManifest, uiBlock, appManifest)

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
	appDir := setupPrebuiltAppDir(t, dir)
	appManifest, err := providerpkg.FindManifestFile(appDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", appDir, err)
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

	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
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
%sapps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(publicPort), publicPort, managementPort, indexedDBManifest, uiBlock, appManifest)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func startCommandAndWaitReadyForURLs(t *testing.T, cmd *exec.Cmd, baseURLs []string) {
	t.Helper()

	startCommandAndWaitReadyForURLsWithOutput(t, cmd, baseURLs, nil)
}

func startCommandAndWaitReadyForURLsWithOutput(t *testing.T, cmd *exec.Cmd, baseURLs []string, output io.Writer) {
	t.Helper()

	cmdOutput := io.Writer(os.Stderr)
	if output != nil {
		cmdOutput = io.MultiWriter(os.Stderr, output)
	}
	cmd.Stdout = cmdOutput
	cmd.Stderr = cmdOutput
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
	ready := make([]bool, len(baseURLs))
	for {
		select {
		case <-exited:
			t.Fatal("gestaltd exited before becoming ready")
		case <-timeout:
			t.Fatalf("gestaltd did not become ready within 60 seconds: %v", baseURLs)
		case <-tick.C:
			allReady := true
			for i, baseURL := range baseURLs {
				if ready[i] {
					continue
				}
				resp, err := client.Get(baseURL + "/ready")
				if err == nil {
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						ready[i] = true
					}
				}
				if !ready[i] {
					allReady = false
				}
			}
			if allReady {
				return
			}
		}
	}
}

func e2eLoopbackBaseURL(port int) string {
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", fmt.Sprint(port)),
	}).String()
}

func TestE2EServeSplitManagementRoutes(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping split management serve test in short mode")
	}

	dir := t.TempDir()
	mountedUI := setupMountedUIDir(t, dir)
	publicPort, publicHolder := reservePort(t)
	managementPort, managementHolder := reservePort(t)
	publicURL := fmt.Sprintf("http://127.0.0.1:%d", publicPort)
	managementURL := fmt.Sprintf("http://127.0.0.1:%d", managementPort)
	cfgPath := writeServeConfigWithManagement(t, dir, publicPort, managementPort, mountedUI)

	cmd := exec.Command(gestaltdBin, "serve", "--config", cfgPath)
	releasePortHoldersAndStart(t, []net.Listener{publicHolder, managementHolder}, func() {
		startCommandAndWaitReadyForURLs(t, cmd, []string{publicURL, managementURL})
	})

	client := &http.Client{Timeout: 2 * time.Second}
	for _, tc := range []struct {
		name         string
		url          string
		wantStatus   int
		wantContains string
	}{
		{
			name:       "public serves apps API",
			url:        publicURL + "/api/v1/apps",
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
			url:        managementURL + "/api/v1/apps",
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
			wantContains: "Prometheus telemetry",
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

func TestE2ELockLocalProviders(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E lock test in short mode")
	}

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)

	out, err := exec.Command(gestaltdBin, "lock", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock failed: %v\noutput: %s", err, out)
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
		t.Fatalf("expected schema-based lockfile without version field, got %v", lock["version"])
	}
	if _, err := os.Stat(filepath.Join(dir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("lock should not prepare artifacts, got err=%v", err)
	}
}

func TestE2ELockAndSyncSkipRuntimeSecretRefs(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping E2E runtime secret lock/sync test in short mode")
	}

	dir := t.TempDir()
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	missingSecretName := "GESTALT_E2E_RUNTIME_SECRET_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  encryptionKey:
    secret:
      provider: env
      name: %s
  providers:
    indexeddb: inmem
providers:
  secrets:
    env:
      source: env
  indexeddb:
    inmem:
      source:
        path: %s
      config:
        dsn: sqlite://%s
`, missingSecretName, indexedDBManifest, filepath.Join(dir, "gestalt.db"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	lockPath := filepath.Join(dir, "gestalt.lock.json")
	out, err := exec.Command(gestaltdBin, "lock", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd lock should not resolve runtime secret refs: %v\n%s", err, out)
	}
	out, err = exec.Command(gestaltdBin, "sync", "--locked", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd sync should not resolve runtime secret refs: %v\n%s", err, out)
	}
	out, err = exec.Command(gestaltdBin, "validate", "--runtime", "--config", cfgPath, "--lockfile", lockPath).CombinedOutput()
	if err == nil {
		t.Fatalf("gestaltd validate --runtime unexpectedly succeeded without runtime secret:\n%s", out)
	}
	if !strings.Contains(string(out), missingSecretName) {
		t.Fatalf("validate --runtime error should mention missing runtime secret %q, got:\n%s", missingSecretName, out)
	}
}

func TestRunLockSyncLocalProviders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)

	if err := runLock([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runLock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); err != nil {
		t.Fatalf("expected default lockfile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gestaltd")); !os.IsNotExist(err) {
		t.Fatalf("lock should not prepare final artifacts, got err=%v", err)
	}
	if err := runLock([]string{"--check", "--config", cfgPath}); err != nil {
		t.Fatalf("runLock --check: %v", err)
	}

	err := runSync([]string{"--locked", "--check", "--config", cfgPath})
	if err == nil {
		t.Fatal("runSync --check before sync unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "gestaltd sync --locked") {
		t.Fatalf("sync --check error should point at sync, got: %v", err)
	}

	if err := runSync([]string{"--locked", "--config", cfgPath}); err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gestaltd", "providers", "example")); err != nil {
		t.Fatalf("expected synced app artifact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "indexeddb", "inmem")); err != nil {
		t.Fatalf("expected synced indexeddb artifact: %v", err)
	}

	if err := runSync([]string{"--locked", "--check", "--config", cfgPath}); err != nil {
		t.Fatalf("runSync --check after sync: %v", err)
	}
}

func TestRunSyncStaleLocalProviderRemediation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "gestalt.lock.json")
	if err := runLock([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runLock: %v", err)
	}

	staleCfgPath := writeRenamedProviderConfig(t, dir, cfgPath)
	err := runSync([]string{"--locked", "--check", "--config", staleCfgPath, "--artifacts-dir", filepath.Join(dir, "prepared")})
	if err == nil {
		t.Fatal("runSync --check with stale lock unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "prepared artifact for provider") ||
		!strings.Contains(err.Error(), "renamed") ||
		!strings.Contains(err.Error(), "gestaltd sync --locked") {
		t.Fatalf("stale local source error should point at sync, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--artifacts-dir") {
		t.Fatalf("sync remediation should include --artifacts-dir, got: %v", err)
	}
	if strings.Contains(err.Error(), "--lockfile") {
		t.Fatalf("sync remediation should not include default --lockfile, got: %v", err)
	}

	staleArtifactsDir := filepath.Join(dir, "prepared-stale")
	if err := runSync([]string{"--locked", "--config", staleCfgPath, "--artifacts-dir", staleArtifactsDir}); err != nil {
		t.Fatalf("runSync with renamed local provider: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staleArtifactsDir, ".gestaltd", "providers", "renamed")); err != nil {
		t.Fatalf("renamed local provider should be prepared from source, stat err=%v", err)
	}

	err = runSync([]string{"--locked", "--check", "--config", staleCfgPath, "--lockfile", lockPath, "--artifacts-dir", filepath.Join(dir, "prepared-explicit")})
	if err == nil {
		t.Fatal("runSync --check with stale explicit lock unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "--lockfile "+lockPath) {
		t.Fatalf("sync remediation should preserve explicit --lockfile, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--artifacts-dir") {
		t.Fatalf("sync remediation should include --artifacts-dir, got: %v", err)
	}
}

func writeRenamedProviderConfig(t *testing.T, dir, cfgPath string) string {
	t.Helper()

	staleCfgPath := filepath.Join(dir, "config-stale.yaml")
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	staleCfg := strings.Replace(string(cfgBytes), "  example:\n    source:", "  renamed:\n    source:", 1)
	if staleCfg == string(cfgBytes) {
		t.Fatal("failed to create stale config")
	}
	if err := os.WriteFile(staleCfgPath, []byte(staleCfg), 0o644); err != nil {
		t.Fatalf("write stale config: %v", err)
	}
	return staleCfgPath
}

func TestRunLockWritesOverrideLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "local", "gestalt.lock.json")

	if err := runLock([]string{"--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock with --lockfile: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected override lockfile at %s: %v", lockPath, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func TestRunLockSyncLayeredConfigs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath, overridePath, lockPath, _ := writeLayeredE2EConfigs(t, dir, 0)

	if err := runLock([]string{"--config", basePath, "--config", overridePath}); err != nil {
		t.Fatalf("runLock with layered configs: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file at %s: %v", lockPath, err)
	}
	if err := runSync([]string{"--locked", "--config", basePath, "--config", overridePath}); err != nil {
		t.Fatalf("runSync with layered configs: %v", err)
	}

	env, err := setupBootstrapWithConfigPaths([]string{basePath, overridePath}, operator.StatePaths{}, true)
	if err != nil {
		t.Fatalf("setupBootstrapWithConfigPaths locked layered configs: %v", err)
	}
	defer env.Close()
	if got := env.Config.Server.Providers.Identity; got != "local" {
		t.Fatalf("Server.Providers.Identity = %q, want local", got)
	}
	if _, auth, err := env.Config.SelectedIdentityProvider(); err != nil || auth == nil {
		t.Fatalf("SelectedIdentityProvider = (%#v, %v), want local auth provider", auth, err)
	}
}

func TestRunServeLockedUsesOverrideLockfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := writeServeConfig(t, dir, 0, nil)
	lockPath := filepath.Join(dir, "state", "locked-serve", "gestalt.lock.json")
	if err := runLock([]string{"--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runLock with --lockfile: %v", err)
	}
	if err := runSync([]string{"--locked", "--config", cfgPath, "--lockfile", lockPath}); err != nil {
		t.Fatalf("runSync with --lockfile: %v", err)
	}

	env, err := setupBootstrapWithConfigPaths([]string{cfgPath}, operator.StatePaths{LockfilePath: lockPath}, true)
	if err != nil {
		t.Fatalf("setupBootstrapWithConfigPaths locked with --lockfile: %v", err)
	}
	defer env.Close()
	if env.Config.Apps["example"] == nil {
		t.Fatal(`Apps["example"] = nil`)
	}
	if _, err := os.Stat(filepath.Join(dir, "gestalt.lock.json")); !os.IsNotExist(err) {
		t.Fatalf("default lockfile should not be written, got err=%v", err)
	}
}

func writeE2EConfig(t *testing.T, dir, appDir string, port int) string {
	t.Helper()
	return writeE2EConfigWithPaths(t, dir, appDir, filepath.Join(dir, "gestalt.db"), "", port)
}

func writeValidValidateConfig(t *testing.T, dir string) string {
	t.Helper()

	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	appManifest := componentProviderManifestPath(t, setupPrebuiltAppDir(t, dir))

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  encryptionKey: valid-config-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(8080), externalCredentialsManifest, indexedDBManifest, appManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write valid config: %v", err)
	}
	return cfgPath
}

func writeInvalidValidateConfig(t *testing.T, path string) {
	t.Helper()

	dir := filepath.Dir(path)
	indexedDBManifest := componentProviderManifestPath(t, setupIndexedDBProviderDir(t, dir))
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	cfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  encryptionKey: invalid-config-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(8080), externalCredentialsManifest, indexedDBManifest, filepath.Join(dir, "missing-app", "manifest.yaml"))
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
	externalCredentialsManifest := componentProviderManifestPath(t, setupExternalCredentialsProviderDir(t, dir))
	appManifest := componentProviderManifestPath(t, setupPrebuiltAppDir(t, dir))
	authManifest := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, "local"))

	indexedDBRel, err := filepath.Rel(deployDir, indexedDBManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(indexeddb): %v", err)
	}
	appRel, err := filepath.Rel(deployDir, appManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(app): %v", err)
	}
	authRel, err := filepath.Rel(overrideDir, authManifest)
	if err != nil {
		t.Fatalf("filepath.Rel(auth): %v", err)
	}

	basePath := filepath.Join(deployDir, "base.yaml")
	overridePath := filepath.Join(overrideDir, "local.yaml")
	baseCfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  public:
    port: %d
  encryptionKey: test-layered-e2e-key
  providers:
    indexeddb: inmem
    externalCredentials: default
providers:
  externalCredentials:
    default:
      source:
        path: %s
  indexeddb:
    inmem:
      source:
        path: %s
apps:
  example:
    source:
      path: %s
`, e2eLoopbackBaseURL(port), port, filepath.ToSlash(externalCredentialsManifest), filepath.ToSlash(indexedDBRel), filepath.ToSlash(appRel))
	overrideCfg := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  providers:
    identity: local
  artifactsDir: ../artifacts/local
providers:
  identity:
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

func writeE2EConfigWithPaths(t *testing.T, dir, appDir, dbPath, artifactsDir string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}
	manifestPath, err := providerpkg.FindManifestFile(appDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", appDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	serverBlock := fmt.Sprintf(`apiVersion: gestaltd.config/v7
server:
  baseUrl: %s
  public:
    port: %d
  encryptionKey: test-e2e-key
`, e2eLoopbackBaseURL(port), port)
	if artifactsDir != "" {
		serverBlock += fmt.Sprintf("  artifactsDir: %s\n", artifactsDir)
	}
	cfg := serverBlock + authIndexedDBConfigYAML(t, dir, "", "sqlite", dbPath) + fmt.Sprintf(`apps:
    example:
      source:
        path: %s
`, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}
