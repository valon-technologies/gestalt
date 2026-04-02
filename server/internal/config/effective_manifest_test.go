package config

import (
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestEffectiveManifestBackedInputsUsesLocalAllowedOperationsForResolvedManifest(t *testing.T) {
	t.Parallel()

	localAllowed := map[string]*OperationOverride{
		"docs.documents.get": {
			Alias: "get_document",
		},
	}

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/test/plugins/google-docs",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "https://example.com/openapi.yaml",
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"docs.documents.batchUpdate": {
					Alias: "batch_update",
				},
			},
		},
	}

	gotManifest, gotAllowed, err := EffectiveManifestBackedInputs("google_docs", &PluginDef{
		ResolvedManifest:  manifest,
		AllowedOperations: localAllowed,
	})
	if err != nil {
		t.Fatalf("EffectiveManifestBackedInputs: %v", err)
	}

	if gotManifest == nil || gotManifest.Provider == nil {
		t.Fatal("expected manifest-backed provider")
	}
	if gotAllowed == nil {
		t.Fatal("expected allowed operations")
	}
	if len(gotAllowed) != 1 {
		t.Fatalf("len(gotAllowed) = %d, want 1", len(gotAllowed))
	}
	if got, ok := gotAllowed["docs.documents.get"]; !ok || got == nil || got.Alias != "get_document" {
		t.Fatalf("gotAllowed[docs.documents.get] = %#v, want alias get_document", got)
	}
	if _, ok := gotAllowed["docs.documents.batchUpdate"]; ok {
		t.Fatal("expected local allowed_operations to replace manifest allowed_operations")
	}
}

func TestEffectiveManifestBackedInputsFallsBackToManifestAllowedOperations(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:  "github.com/test/plugins/google-drive",
		Version: "0.1.0",
		Kinds:   []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			OpenAPI: "https://example.com/openapi.yaml",
			AllowedOperations: map[string]*pluginmanifestv1.ManifestOperationOverride{
				"drive.files.list": {
					Alias: "files.list",
				},
			},
		},
	}

	_, gotAllowed, err := EffectiveManifestBackedInputs("google_drive", &PluginDef{
		ResolvedManifest: manifest,
	})
	if err != nil {
		t.Fatalf("EffectiveManifestBackedInputs: %v", err)
	}

	if gotAllowed == nil {
		t.Fatal("expected allowed operations from manifest")
	}
	got, ok := gotAllowed["drive.files.list"]
	if !ok || got == nil {
		t.Fatalf("gotAllowed[drive.files.list] missing: %#v", gotAllowed)
	}
	if got.Alias != "files.list" {
		t.Fatalf("got alias = %q, want files.list", got.Alias)
	}
}
