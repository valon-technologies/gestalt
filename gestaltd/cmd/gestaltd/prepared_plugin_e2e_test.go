package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestE2EValidateUsesUpdatedManagedPluginConfigAfterInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	packagePath := buildPreparedPluginPackageRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.1.0")
	cfgPath := writePreparedPackageConfig(t, dir, packagePath, map[string]string{
		"api_key": "one",
	}, []string{
		"encryption_key: test-key",
	})

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	writePreparedPackageConfig(t, dir, packagePath, map[string]string{
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
	packagePath := buildPreparedPluginPackageRequiringAPIKey(t, dir, "github.com/acme/plugins/provider", "0.1.0")
	cfgPath := writePreparedPackageConfig(t, dir, packagePath, map[string]string{
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
	homeDir := filepath.Join(t.TempDir(), "home:with#special")
	configPath := filepath.Join(homeDir, ".gestaltd", "config.yaml")

	cmd := exec.Command(gestaltdBin)
	cmd.Env = withoutEnvVar(os.Environ(), "GESTALT_CONFIG")
	cmd.Env = append(cmd.Env, "HOME="+homeDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}

	waitForFile(t, configPath, 20*time.Second)
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	out, err := exec.Command(gestaltdBin, "validate", "--config", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd validate: %v\n%s", err, out)
	}
}

func buildPreparedPluginPackageRequiringAPIKey(t *testing.T, dir, source, version string) string {
	t.Helper()

	srcDir := filepath.Join(dir, "plugin-src")
	artifactRel := pluginArtifactRel()
	artifactAbs := filepath.Join(srcDir, filepath.FromSlash(artifactRel))
	schemaRel := "schemas/config.schema.json"
	schema := `{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`

	if err := os.MkdirAll(filepath.Dir(artifactAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll artifact dir: %v", err)
	}
	if err := copyFile(pluginBin, artifactAbs); err != nil {
		t.Fatalf("copy plugin binary: %v", err)
	}
	writeTestFile(t, srcDir, schemaRel, []byte(schema), 0o644)

	digest, err := fileSHA256(artifactAbs)
	if err != nil {
		t.Fatalf("hash plugin artifact: %v", err)
	}

	writeManifestFile(t, srcDir, &pluginmanifestv1.Manifest{
		Source:  source,
		Version: version,
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			ConfigSchemaPath: schemaRel,
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     runtime.GOOS,
				Arch:   runtime.GOARCH,
				Path:   artifactRel,
				SHA256: digest,
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactRel},
		},
	})

	archivePath := filepath.Join(dir, "plugin.tar.gz")
	if err := pluginpkg.CreatePackageFromDir(srcDir, archivePath); err != nil {
		t.Fatalf("CreatePackageFromDir: %v", err)
	}
	return archivePath
}

func writePreparedPackageConfig(t *testing.T, dir, packagePath string, pluginConfig map[string]string, serverLines []string) string {
	t.Helper()

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
			configBlock.WriteString(fmt.Sprintf("      %s: %q\n", key, pluginConfig[key]))
		}
	}

	cfg := fmt.Sprintf(`auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: %s
%sproviders:
  example:
    from:
      package: %s
%s`, filepath.Join(dir, "gestalt.db"), serverBlock.String(), packagePath, configBlock.String())
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
