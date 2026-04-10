package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

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
		"encryptionKey: test-key",
	})

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	writePreparedSourceConfig(t, dir, pluginDir, map[string]string{
		"wrong_key": "two",
	}, []string{
		"encryptionKey: test-key",
	})

	out, err = exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail after managed plugin config changed, output: %s", out)
	}
	if !strings.Contains(string(out), "api_key") {
		t.Fatalf("expected output to mention missing api_key, got: %s", out)
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

