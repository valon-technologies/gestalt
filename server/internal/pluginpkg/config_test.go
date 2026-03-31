package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
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
		Source:  "github.com/acme/plugins/provider",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
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

	if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindProvider, map[string]any{"api_key": "sk-test"}); err != nil {
		t.Fatalf("ValidateConfigForManifest(valid): %v", err)
	}
	if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindProvider, map[string]any{"missing": true}); err == nil {
		t.Fatal("expected schema validation failure")
	}
}

func TestValidateConfigForManifestRuntimeFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, ManifestFile)
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(RuntimeConfigSchemaPath)), []byte(`{
  "type": "object",
  "required": ["runtime_key"],
  "properties": {
    "runtime_key": { "type": "string" }
  }
}`), 0644); err != nil {
		t.Fatalf("WriteFile(schema): %v", err)
	}
	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/runtime",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindRuntime},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "darwin",
				Arch:   "arm64",
				Path:   "artifacts/darwin/arm64/runtime",
				SHA256: sha256Hex("runtime"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Runtime: &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/darwin/arm64/runtime"},
		},
	}
	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindRuntime, map[string]any{"runtime_key": "value"}); err != nil {
		t.Fatalf("ValidateConfigForManifest(runtime valid): %v", err)
	}
	if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindRuntime, map[string]any{"missing": true}); err == nil {
		t.Fatal("expected runtime schema validation failure")
	}
}

func TestValidateConfigForManifestRuntimeDoesNotUseProviderSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, ManifestFile)
	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/acme/plugins/plugin",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider, pluginmanifestv1.KindRuntime},
		Provider: &pluginmanifestv1.Provider{
			ConfigSchemaPath: "schemas/provider.schema.json",
		},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "darwin",
				Arch:   "arm64",
				Path:   "artifacts/darwin/arm64/plugin",
				SHA256: sha256Hex("plugin"),
			},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/darwin/arm64/plugin"},
			Runtime:  &pluginmanifestv1.Entrypoint{ArtifactPath: "artifacts/darwin/arm64/plugin"},
		},
	}
	data, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatalf("EncodeManifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		t.Fatalf("WriteFile(manifest): %v", err)
	}

	if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindRuntime, map[string]any{"missing": true}); err != nil {
		t.Fatalf("ValidateConfigForManifest(runtime without fallback schema): %v", err)
	}
}
