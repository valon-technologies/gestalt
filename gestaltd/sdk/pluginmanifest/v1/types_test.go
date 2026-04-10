package pluginmanifestv1

import "testing"

func TestPluginResolvedMode(t *testing.T) {
	tests := []struct {
		name          string
		plugin        *Plugin
		hasEntrypoint bool
		want          PluginMode
	}{
		{
			name:          "nil plugin returns executable",
			plugin:        nil,
			hasEntrypoint: false,
			want:          PluginModeExecutable,
		},
		{
			name:          "explicit mode returned as-is",
			plugin:        &Plugin{Mode: PluginModeHybrid},
			hasEntrypoint: false,
			want:          PluginModeHybrid,
		},
		{
			name: "no mode + REST operations + no entrypoint = declarative",
			plugin: &Plugin{
				Surfaces: &PluginSurfaces{
					REST: &RESTSurface{
						BaseURL:    "https://api.example.com",
						Operations: []ProviderOperation{{Name: "list", Method: "GET", Path: "/items"}},
					},
				},
			},
			hasEntrypoint: false,
			want:          PluginModeDeclarative,
		},
		{
			name: "no mode + OpenAPI surface + no entrypoint = spec-loaded",
			plugin: &Plugin{
				Surfaces: &PluginSurfaces{
					OpenAPI: &OpenAPISurface{Document: "openapi.yaml"},
				},
			},
			hasEntrypoint: false,
			want:          PluginModeSpecLoaded,
		},
		{
			name: "no mode + entrypoint + surfaces = hybrid",
			plugin: &Plugin{
				Surfaces: &PluginSurfaces{
					REST: &RESTSurface{
						BaseURL:    "https://api.example.com",
						Operations: []ProviderOperation{{Name: "list", Method: "GET", Path: "/items"}},
					},
				},
			},
			hasEntrypoint: true,
			want:          PluginModeHybrid,
		},
		{
			name:          "no mode + entrypoint only = executable",
			plugin:        &Plugin{},
			hasEntrypoint: true,
			want:          PluginModeExecutable,
		},
		{
			name:          "no mode + no entrypoint + no surfaces = executable",
			plugin:        &Plugin{},
			hasEntrypoint: false,
			want:          PluginModeExecutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.plugin.ResolvedMode(tt.hasEntrypoint)
			if got != tt.want {
				t.Errorf("ResolvedMode(%v) = %q, want %q", tt.hasEntrypoint, got, tt.want)
			}
		})
	}
}
