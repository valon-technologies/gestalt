package bootstrap

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestResolveManifestRelativeSpecURL(t *testing.T) {
	t.Parallel()

	plugin := &config.ProviderDef{
		ResolvedManifestPath: filepath.Join("/opt", "providers", "github", "manifest.yaml"),
	}

	tests := []struct {
		name    string
		plugin  *config.ProviderDef
		raw     string
		want    string
		wantErr string
	}{
		{
			name:   "HTTP passthrough",
			plugin: plugin,
			raw:    "https://api.example.com/openapi.yaml",
			want:   "https://api.example.com/openapi.yaml",
		},
		{
			name:   "HTTPS passthrough",
			plugin: plugin,
			raw:    "http://api.example.com/openapi.yaml",
			want:   "http://api.example.com/openapi.yaml",
		},
		{
			name:   "relative OK",
			plugin: plugin,
			raw:    "openapi.yaml",
			want:   filepath.Join("/opt", "providers", "github", "openapi.yaml"),
		},
		{
			name:   "nested relative OK",
			plugin: plugin,
			raw:    "specs/openapi.yaml",
			want:   filepath.Join("/opt", "providers", "github", "specs", "openapi.yaml"),
		},
		{
			name:   "file relative OK",
			plugin: plugin,
			raw:    "file://openapi.yaml",
			want:   "file://" + filepath.Join("/opt", "providers", "github", "openapi.yaml"),
		},
		{
			name:    "traversal rejected",
			plugin:  plugin,
			raw:     "../../etc/passwd",
			wantErr: "escapes the manifest directory",
		},
		{
			name:    "file traversal rejected",
			plugin:  plugin,
			raw:     "file://../../etc/passwd",
			wantErr: "escapes the manifest directory",
		},
		{
			name:    "absolute path rejected",
			plugin:  plugin,
			raw:     "/etc/passwd",
			wantErr: "must be relative to the manifest directory",
		},
		{
			name:    "file absolute rejected",
			plugin:  plugin,
			raw:     "file:///etc/passwd",
			wantErr: "must be relative to the manifest directory",
		},
		{
			name:   "nil plugin passthrough",
			plugin: nil,
			raw:    "../../../etc/passwd",
			want:   "../../../etc/passwd",
		},
		{
			name:   "empty manifest path passthrough",
			plugin: &config.ProviderDef{},
			raw:    "../../../etc/passwd",
			want:   "../../../etc/passwd",
		},
		{
			name:   "empty raw passthrough",
			plugin: plugin,
			raw:    "",
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveManifestRelativeSpecURL(tc.plugin, tc.raw)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
