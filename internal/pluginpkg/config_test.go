package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/sdk/pluginmanifest/v1"
)

func TestValidateConfigForManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, ManifestFile)
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schemas", "config.schema.json"), []byte(`{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`), 0644); err != nil {
		t.Fatalf("WriteFile(schema): %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		SchemaVersion: pluginmanifestv1.SchemaVersion,
		ID:            "acme/provider",
		Version:       "0.1.0",
		Kinds:         []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			Protocol:         pluginmanifestv1.ProtocolRange{Min: 1, Max: 1},
			ConfigSchemaPath: "schemas/config.schema.json",
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "darwin",
				Arch:   "arm64",
				Path:   "artifacts/darwin/arm64/provider",
				SHA256: sha256Hex("provider"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/darwin/arm64/provider"},
		},
	}
	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	if err := ValidateConfigForManifest(manifestPath, manifest, map[string]any{"api_key": "sk-test"}); err != nil {
		t.Fatalf("ValidateConfigForManifest(valid): %v", err)
	}
	if err := ValidateConfigForManifest(manifestPath, manifest, map[string]any{"missing": true}); err == nil {
		t.Fatal("expected schema validation failure")
	}
}
