package appregistry

import (
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestResolveConfigAppEntry_matches_registry_app_name_to_config_key(t *testing.T) {
	t.Parallel()

	entry := &config.ProviderEntry{
		ResolvedManifest: &providermanifestv1.Manifest{
			Source:      "github.com/valon-technologies/valon-tools/apps/g-issues",
			DisplayName: "g-issues",
		},
	}
	entry.Source.SetResolvedPackage("", "0.0.0-config")

	got := resolveConfigAppEntry(map[string]*config.ProviderEntry{
		"gIssues": entry,
	}, "g-issues")
	if got != entry {
		t.Fatalf("resolveConfigAppEntry = %#v, want %#v", got, entry)
	}
}

func TestResolveConfigAppEntry_prefers_direct_config_key_match(t *testing.T) {
	t.Parallel()

	entry := &config.ProviderEntry{}
	entry.Source.SetResolvedPackage("", "0.0.0-config")

	got := resolveConfigAppEntry(map[string]*config.ProviderEntry{
		"g-issues": entry,
	}, "g-issues")
	if got != entry {
		t.Fatalf("resolveConfigAppEntry = %#v, want %#v", got, entry)
	}
}
