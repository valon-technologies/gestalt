package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestProviderDevTargetUIPathRequiresPluginOwnedMountedUI(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			UI: map[string]*config.UIEntry{
				"roadmap": {
					Path: "/standalone",
				},
				"owned": {
					Path:        "/owned",
					OwnerPlugin: "roadmap",
				},
				"other": {
					Path:        "/other",
					OwnerPlugin: "other-plugin",
				},
			},
		},
	}

	cases := []struct {
		name  string
		entry *config.ProviderEntry
		want  string
	}{
		{
			name: "standalone ui with same key is not provider-dev interceptable",
			entry: &config.ProviderEntry{
				MountPath: "/standalone",
			},
			want: "",
		},
		{
			name: "plugin-owned ui with default key",
			entry: &config.ProviderEntry{
				MountPath: "/owned",
				UI:        "owned",
			},
			want: "/owned",
		},
		{
			name: "ui owned by another plugin is not provider-dev interceptable",
			entry: &config.ProviderEntry{
				MountPath: "/other",
				UI:        "other",
			},
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := providerDevTargetUIPath("roadmap", tc.entry, cfg); got != tc.want {
				t.Fatalf("providerDevTargetUIPath = %q, want %q", got, tc.want)
			}
		})
	}
}
