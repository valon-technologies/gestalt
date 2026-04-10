package pluginpkg

import (
	"path/filepath"
	"strings"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestResolveManifestLocalReferences(t *testing.T) {
	t.Parallel()

	manifestPath := filepath.Join("/opt", "providers", "github", "manifest.yaml")

	t.Run("normal relative resolution", func(t *testing.T) {
		t.Parallel()
		manifest := &pluginmanifestv1.Manifest{
			Plugin: &pluginmanifestv1.Plugin{
				OpenAPI: "openapi.yaml",
			},
		}
		got, err := ResolveManifestLocalReferences(manifest, manifestPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join("/opt", "providers", "github", "openapi.yaml")
		if got.Plugin.OpenAPI != want {
			t.Fatalf("OpenAPI = %q, want %q", got.Plugin.OpenAPI, want)
		}
	})

	t.Run("HTTP URL unchanged", func(t *testing.T) {
		t.Parallel()
		manifest := &pluginmanifestv1.Manifest{
			Plugin: &pluginmanifestv1.Plugin{
				OpenAPI: "https://api.example.com/openapi.yaml",
			},
		}
		got, err := ResolveManifestLocalReferences(manifest, manifestPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Plugin.OpenAPI != "https://api.example.com/openapi.yaml" {
			t.Fatalf("OpenAPI = %q, want unchanged", got.Plugin.OpenAPI)
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		t.Parallel()
		manifest := &pluginmanifestv1.Manifest{
			Plugin: &pluginmanifestv1.Plugin{
				OpenAPI: "../../../etc/passwd",
			},
		}
		_, err := ResolveManifestLocalReferences(manifest, manifestPath)
		if err == nil {
			t.Fatal("expected error for path traversal")
		}
		if !strings.Contains(err.Error(), "escapes the manifest directory") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil manifest", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveManifestLocalReferences(nil, manifestPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatal("expected nil manifest")
		}
	})

	t.Run("absolute path unchanged", func(t *testing.T) {
		t.Parallel()
		manifest := &pluginmanifestv1.Manifest{
			Plugin: &pluginmanifestv1.Plugin{
				OpenAPI: "/absolute/path/openapi.yaml",
			},
		}
		got, err := ResolveManifestLocalReferences(manifest, manifestPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Plugin.OpenAPI != "/absolute/path/openapi.yaml" {
			t.Fatalf("OpenAPI = %q, want unchanged for absolute path", got.Plugin.OpenAPI)
		}
	})
}
