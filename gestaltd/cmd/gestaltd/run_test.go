package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestResolveMountedWebUIHandlers_PreservesPluginOwnership(t *testing.T) {
	t.Parallel()

	newAssetRoot := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ui</html>"), 0o644); err != nil {
			t.Fatalf("WriteFile index.html: %v", err)
		}
		return dir
	}

	t.Run("explicit plugin ui binding", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: config.ProvidersConfig{
				UI: map[string]*config.UIEntry{
					"roadmap": {
						ProviderEntry: config.ProviderEntry{
							ResolvedAssetRoot:   newAssetRoot(t),
							AuthorizationPolicy: "roadmap_policy",
						},
						Path: "/roadmap",
					},
				},
			},
			Plugins: map[string]*config.ProviderEntry{
				"roadmap_plugin": {
					UI:                  "roadmap",
					MountPath:           "/roadmap",
					AuthorizationPolicy: "roadmap_policy",
				},
			},
		}

		mounted, err := resolveMountedWebUIHandlers(cfg)
		if err != nil {
			t.Fatalf("resolveMountedWebUIHandlers: %v", err)
		}
		if len(mounted) != 1 {
			t.Fatalf("len(mounted) = %d, want 1", len(mounted))
		}
		if got := mounted[0].PluginName; got != "roadmap_plugin" {
			t.Fatalf("mounted[0].PluginName = %q, want %q", got, "roadmap_plugin")
		}
	})

	t.Run("synthesized plugin owned ui", func(t *testing.T) {
		t.Parallel()

		cfg := &config.Config{
			Providers: config.ProvidersConfig{
				UI: map[string]*config.UIEntry{
					"roadmap": {
						ProviderEntry: config.ProviderEntry{
							ResolvedAssetRoot:   newAssetRoot(t),
							AuthorizationPolicy: "roadmap_policy",
						},
						Path: "/roadmap",
					},
				},
			},
			Plugins: map[string]*config.ProviderEntry{
				"roadmap": {
					MountPath:           "/roadmap",
					AuthorizationPolicy: "roadmap_policy",
				},
			},
		}

		mounted, err := resolveMountedWebUIHandlers(cfg)
		if err != nil {
			t.Fatalf("resolveMountedWebUIHandlers: %v", err)
		}
		if len(mounted) != 1 {
			t.Fatalf("len(mounted) = %d, want 1", len(mounted))
		}
		if got := mounted[0].PluginName; got != "roadmap" {
			t.Fatalf("mounted[0].PluginName = %q, want %q", got, "roadmap")
		}
	})
}
