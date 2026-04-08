package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestE2EValidateUsesUpdatedManagedPluginConfigAfterInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := buildPreparedPluginRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.0.1-alpha.1")
	cfgPath := writePreparedSourceConfig(t, dir, pluginDir, map[string]string{
		"api_key": "one",
	}, []string{
		"encryption_key: test-key",
	})

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	writePreparedSourceConfig(t, dir, pluginDir, map[string]string{
		"wrong_key": "two",
	}, []string{
		"encryption_key: test-key",
	})

	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail after managed plugin config changed, output: %s", out)
	}
	if !strings.Contains(string(out), "api_key") {
		t.Fatalf("expected output to mention missing api_key, got: %s", out)
	}
}

func TestE2EServeLockedResolvesLateBoundManagedPluginEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	apiKeyEnv := "TEST_API_KEY_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "/", "_"))
	portEnv := apiKeyEnv + "_PORT"
	pluginDir := buildPreparedPluginRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.0.1-alpha.1")
	cfgPath := writePreparedSourceConfig(t, dir, pluginDir, map[string]string{
		"api_key": "${" + apiKeyEnv + "}",
	}, []string{
		"port: ${" + portEnv + "}",
		"encryption_key: test-key",
	})

	initCmd := exec.Command(gestaltdBin, "init", "--config", cfgPath)
	initCmd.Env = withoutEnvVar(withoutEnvVar(os.Environ(), apiKeyEnv), portEnv)
	out, err := initCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	port := allocateTestPort(t)
	cmd := exec.Command(gestaltdBin, "serve", "--locked", "--config", cfgPath)
	cmd.Env = withoutEnvVar(withoutEnvVar(os.Environ(), apiKeyEnv), portEnv)
	cmd.Env = append(cmd.Env,
		apiKeyEnv+"=runtime-value",
		fmt.Sprintf("%s=%d", portEnv, port),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)
}

func TestE2EDefaultStartAutoGeneratesHomeConfig(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	repoDir := filepath.Join(workspaceDir, "gestalt")
	providersDir := filepath.Join(workspaceDir, "gestalt-providers")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll repo dir: %v", err)
	}
	homeDir := filepath.Join(t.TempDir(), "home:with#special")
	_ = setupSourceOnlyDatastoreProviderDir(t, providersDir, "sqlite")
	configPath := filepath.Join(homeDir, ".gestaltd", "config.yaml")
	legacyConfigDir := filepath.Join(homeDir, ".gestalt")
	if err := os.MkdirAll(legacyConfigDir, 0o755); err != nil {
		t.Fatalf("MkdirAll legacy config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyConfigDir, "config.yaml"), []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatalf("WriteFile legacy config: %v", err)
	}

	cmd := exec.Command(gestaltdBin)
	cmd.Dir = repoDir
	cmd.Env = withoutEnvVar(withoutEnvVar(os.Environ(), "GESTALT_CONFIG"), "GESTALT_PROVIDERS_DIR")
	cmd.Env = append(cmd.Env,
		"HOME="+homeDir,
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	})

	waitForFile(t, configPath, 20*time.Second)
	stopped = true
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	cfgText := string(cfgBytes)
	if !strings.Contains(cfgText, filepath.Join(providersDir, "datastore", "sqlite", "plugin.yaml")) {
		t.Fatalf("expected generated config to use sibling local datastore source, config: %s", cfgText)
	}

	validateCmd := exec.Command(gestaltdBin, "validate", "--config", configPath)
	validateCmd.Dir = repoDir
	validateCmd.Env = append(withoutEnvVar(os.Environ(), "GESTALT_PROVIDERS_DIR"),
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	out, err := validateCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate: %v\n%s", err, out)
	}
}

func TestE2EDefaultStartMigratesLegacyHomeConfig(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	repoDir := filepath.Join(workspaceDir, "gestalt")
	providersDir := filepath.Join(workspaceDir, "gestalt-providers")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll repo dir: %v", err)
	}
	homeDir := filepath.Join(t.TempDir(), "home:with#special")
	_ = setupSourceOnlyDatastoreProviderDir(t, providersDir, "sqlite")

	configDir := filepath.Join(homeDir, ".gestaltd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}

	port := allocateTestPort(t)
	configPath := filepath.Join(configDir, "config.yaml")
	legacyConfig := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %q
secrets:
  provider: env
server:
  port: %d
  encryption_key: test-key
`, filepath.Join(configDir, "gestalt.db"), port)
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0o600); err != nil {
		t.Fatalf("WriteFile legacy config: %v", err)
	}

	cmd := exec.Command(gestaltdBin)
	cmd.Dir = repoDir
	cmd.Env = withoutEnvVar(withoutEnvVar(os.Environ(), "GESTALT_CONFIG"), "GESTALT_PROVIDERS_DIR")
	cmd.Env = append(cmd.Env,
		"HOME="+homeDir,
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)
	stopped = true
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	cfgText := string(cfgBytes)
	if strings.Contains(cfgText, "provider: sqlite") {
		t.Fatalf("expected legacy datastore provider scalar to be migrated, config: %s", cfgText)
	}
	if !strings.Contains(cfgText, filepath.Join(providersDir, "datastore", "sqlite", "plugin.yaml")) {
		t.Fatalf("expected migrated config to use sibling local datastore source, config: %s", cfgText)
	}

	validateCmd := exec.Command(gestaltdBin, "validate", "--config", configPath)
	validateCmd.Dir = repoDir
	validateCmd.Env = append(withoutEnvVar(os.Environ(), "GESTALT_PROVIDERS_DIR"),
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	out, err := validateCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate: %v\n%s", err, out)
	}
}

func TestE2EDefaultStartRepairsManagedHomeConfig(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	repoDir := filepath.Join(workspaceDir, "gestalt")
	providersDir := filepath.Join(workspaceDir, "gestalt-providers")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll repo dir: %v", err)
	}
	_ = setupSourceOnlyDatastoreProviderDir(t, providersDir, "sqlite")

	homeDir := filepath.Join(t.TempDir(), "home:with#special")
	configDir := filepath.Join(homeDir, ".gestaltd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}

	port := allocateTestPort(t)
	configPath := filepath.Join(configDir, "config.yaml")
	managedConfig := fmt.Sprintf(`datastore:
  provider:
    source:
      ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
      version: 0.0.1-alpha.1
  config:
    path: %q
secrets:
  provider: env
server:
  port: %d
  encryption_key: test-key
`, filepath.Join(configDir, "gestalt.db"), port)
	if err := os.WriteFile(configPath, []byte(managedConfig), 0o600); err != nil {
		t.Fatalf("WriteFile managed config: %v", err)
	}

	cmd := exec.Command(gestaltdBin)
	cmd.Dir = repoDir
	cmd.Env = withoutEnvVar(withoutEnvVar(os.Environ(), "GESTALT_CONFIG"), "GESTALT_PROVIDERS_DIR")
	cmd.Env = append(cmd.Env,
		"HOME="+homeDir,
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)
	stopped = true
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	cfgBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read repaired config: %v", err)
	}
	cfgText := string(cfgBytes)
	if strings.Contains(cfgText, "github.com/valon-technologies/gestalt-providers/datastore/sqlite") {
		t.Fatalf("expected repaired config to stop using managed datastore ref, config: %s", cfgText)
	}
	if !strings.Contains(cfgText, filepath.Join(providersDir, "datastore", "sqlite", "plugin.yaml")) {
		t.Fatalf("expected repaired config to use sibling local datastore source, config: %s", cfgText)
	}

	validateCmd := exec.Command(gestaltdBin, "validate", "--config", configPath)
	validateCmd.Dir = repoDir
	validateCmd.Env = append(withoutEnvVar(os.Environ(), "GESTALT_PROVIDERS_DIR"),
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	out, err := validateCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate: %v\n%s", err, out)
	}
}

func buildPreparedPluginRequiringAPIKey(t *testing.T, dir, source, version string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "plugin-src")
	schemaRel := "schemas/config.schema.json"
	schema := `{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`

	testutil.CopyExampleProviderPlugin(t, srcDir)
	writeTestFile(t, srcDir, schemaRel, []byte(schema), 0o644)
	manifest := &pluginmanifestv1.Manifest{
		Source:      source,
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Kinds:       []string{pluginmanifestv1.KindPlugin},
		Plugin: &pluginmanifestv1.Plugin{
			ConfigSchemaPath: schemaRel,
		},
	}
	writeManifestFile(t, srcDir, manifest)

	return srcDir
}

func writePreparedSourceConfig(t *testing.T, dir, pluginDir string, pluginConfig map[string]string, serverLines []string) string {
	t.Helper()
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	var serverBlock strings.Builder
	serverBlock.WriteString("server:\n")
	for _, line := range serverLines {
		serverBlock.WriteString("  ")
		serverBlock.WriteString(line)
		serverBlock.WriteByte('\n')
	}

	var configBlock strings.Builder
	if len(pluginConfig) > 0 {
		configBlock.WriteString("    config:\n")
		keys := make([]string, 0, len(pluginConfig))
		for key := range pluginConfig {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(&configBlock, "      %s: %q\n", key, pluginConfig[key]); err != nil {
				t.Fatalf("write plugin config block: %v", err)
			}
		}
	}

	cfg := authDatastoreConfigYAML(t, dir, "none", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`%splugins:
  example:
    provider:
      source:
        path: %s
%s`, serverBlock.String(), manifestPath, configBlock.String())
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s was not created within %s", path, timeout)
}
