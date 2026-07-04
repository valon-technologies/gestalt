package config

import (
	"strings"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestDeprecationWarnings(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: ProvidersConfig{
			UI: map[string]*UIEntry{
				"legacy": {Path: "/legacy"},
			},
		},
		Apps: map[string]*ProviderEntry{
			"bundled": {
				UI:        "legacy",
				MountPath: "/bundled",
			},
			"owned": {
				MountPath: "/owned",
				ResolvedManifest: &providermanifestv1.Manifest{
					Kind: providermanifestv1.KindApp,
					Spec: &providermanifestv1.Spec{
						UI: &providermanifestv1.OwnedUI{Path: "ui"},
					},
				},
			},
			"modern": {
				Static: &AppStaticConfig{Mount: "/modern"},
			},
		},
	}

	got := cfg.DeprecationWarnings()
	wantSubstrings := []string{
		`providers.ui.legacy is deprecated; migrate to apps.legacy.static`,
		`apps.bundled.ui is deprecated; migrate to apps.bundled.static`,
		`apps.owned manifest spec.ui is deprecated; migrate to apps.owned.static`,
	}
	for _, want := range wantSubstrings {
		if !containsWarning(got, want) {
			t.Fatalf("DeprecationWarnings() = %q, missing %q", got, want)
		}
	}
	for _, warning := range got {
		if strings.Contains(warning, "apps.modern") {
			t.Fatalf("DeprecationWarnings() = %q, want no warning for apps.modern.static", got)
		}
	}
}

func containsWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if warning == want {
			return true
		}
	}
	return false
}
