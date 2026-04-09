package pluginpkg

import (
	"os"
	"path/filepath"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestValidateConfigForManifest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		schemaPath string
		schemaData string
	}{
		{
			name:       "json schema",
			schemaPath: "schemas/config.schema.json",
			schemaData: `{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": { "type": "string" }
  }
}`,
		},
		{
			name:       "yaml schema",
			schemaPath: "schemas/config.schema.yaml",
			schemaData: `type: object
required:
  - api_key
properties:
  api_key:
    type: string
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			manifestPath := filepath.Join(dir, ManifestFile)
			artifactPath := testArtifactPath("provider")
			if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(tc.schemaPath)), []byte(tc.schemaData), 0644); err != nil {
				t.Fatalf("WriteFile(schema): %v", err)
			}
			manifest := &pluginmanifestv1.Manifest{
				Source:  "github.com/acme/plugins/provider",
				Version: "0.0.1-alpha.1",
				Plugin: &pluginmanifestv1.Plugin{
					ConfigSchemaPath: tc.schemaPath,
				},
				Artifacts: []pluginmanifestv1.Artifact{
					{
						OS:     testArtifactOS,
						Arch:   testArtifactArch,
						Path:   artifactPath,
						SHA256: sha256Hex("provider"),
					},
				},
				Entrypoints: pluginmanifestv1.Entrypoints{
					Provider: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactPath},
				},
			}
			data, err := EncodeManifest(manifest)
			if err != nil {
				t.Fatalf("EncodeManifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, data, 0644); err != nil {
				t.Fatalf("WriteFile(manifest): %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte("name: provider\noperations:\n  - id: echo\n    method: POST\n"), 0644); err != nil {
				t.Fatalf("WriteFile(catalog): %v", err)
			}

			if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindPlugin, map[string]any{"api_key": "sk-test"}); err != nil {
				t.Fatalf("ValidateConfigForManifest(valid): %v", err)
			}
			if err := ValidateConfigForManifest(manifestPath, manifest, pluginmanifestv1.KindPlugin, map[string]any{"missing": true}); err == nil {
				t.Fatal("expected schema validation failure")
			}
		})
	}
}
