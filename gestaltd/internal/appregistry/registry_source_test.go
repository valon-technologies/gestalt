package appregistry

import "testing"

func TestEntrySourceTreeURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry Entry
		want  string
	}{
		{
			name: "github app",
			entry: Entry{
				App:        "g-issues",
				SourceRef:  "abc123def456abc123def456abc123def456abcd",
				Repository: "github.com/valon-technologies/valon-tools",
			},
			want: "https://github.com/valon-technologies/valon-tools/tree/abc123def456abc123def456abc123def456abcd/apps/g-issues",
		},
		{
			name: "app in a repository subdirectory",
			entry: Entry{
				App:        "standard-reporting",
				SourceRef:  "abc123def456abc123def456abc123def456abcd",
				Repository: "github.com/valon-technologies/toolshed",
				SourceDir:  "valon-tools/apps/standard-reporting",
			},
			want: "https://github.com/valon-technologies/toolshed/tree/abc123def456abc123def456abc123def456abcd/valon-tools/apps/standard-reporting",
		},
		{
			name: "non github repository",
			entry: Entry{
				App:        "g-issues",
				SourceRef:  "main",
				Repository: "gitlab.example.com/acme/valon-tools",
			},
		},
		{
			name: "www github repository",
			entry: Entry{
				App:        "g-issues",
				SourceRef:  "release/2026-09",
				Repository: "https://www.github.com/valon-technologies/valon-tools.git",
			},
			want: "https://github.com/valon-technologies/valon-tools/tree/release%2F2026-09/apps/g-issues",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.entry.SourceTreeURL(); got != tt.want {
				t.Fatalf("SourceTreeURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAppSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		want    AppSource
		wantErr bool
	}{
		{
			name:   "default directory",
			source: "github.com/valon-technologies/valon-tools/apps/g-issues",
			want:   AppSource{App: "g-issues", Repository: "github.com/valon-technologies/valon-tools"},
		},
		{
			name:   "repository subdirectory",
			source: "github.com/valon-technologies/toolshed/valon-tools/apps/g-issues",
			want:   AppSource{App: "g-issues", Repository: "github.com/valon-technologies/toolshed", SourceDir: "valon-tools/apps/g-issues"},
		},
		{name: "not under apps", source: "github.com/valon-technologies/toolshed/valon-tools/g-issues", wantErr: true},
		{name: "nested app name", source: "github.com/valon-technologies/toolshed/apps/a/b", wantErr: true},
		{name: "parent traversal segment", source: "github.com/valon-technologies/toolshed/a/../apps/g-issues", wantErr: true},
		{name: "empty segment", source: "github.com/valon-technologies/toolshed/a//apps/g-issues", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAppSource(tt.source)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAppSource(%q) error = %v, wantErr %v", tt.source, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseAppSource(%q) = %#v, want %#v", tt.source, got, tt.want)
			}
		})
	}
}

func TestValidateManifestLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		relPath string
		wantErr bool
	}{
		{name: "default source accepts any location", source: "github.com/valon-technologies/valon-tools/apps/x", relPath: "valon-tools/apps/x/manifest.yaml"},
		{name: "declared folder matches", source: "github.com/valon-technologies/toolshed/valon-tools/apps/x", relPath: "valon-tools/apps/x/manifest.yaml"},
		{name: "declared folder differs", source: "github.com/valon-technologies/toolshed/a/apps/x", relPath: "b/apps/x/manifest.yaml", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateManifestLocation(tt.source, tt.relPath); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateManifestLocation() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSourceDir(t *testing.T) {
	t.Parallel()

	const repository = "github.com/valon-technologies/toolshed"
	tests := []struct {
		name      string
		app       string
		sourceDir string
		wantErr   bool
	}{
		{name: "empty is the default", app: "x"},
		{name: "own subfolder", app: "x", sourceDir: "valon-tools/apps/x"},
		{name: "another app's folder", app: "x", sourceDir: "valon-tools/apps/y", wantErr: true},
		{name: "default spelled out", app: "x", sourceDir: "apps/x", wantErr: true},
		{name: "traversal", app: "x", sourceDir: "../evil/apps/x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateSourceDir(repository, tt.app, tt.sourceDir); (err != nil) != tt.wantErr {
				t.Fatalf("validateSourceDir() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
