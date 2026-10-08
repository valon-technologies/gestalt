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

func TestSourceDirFromManifestSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		want    string
		wantErr bool
	}{
		{name: "default directory", source: "github.com/valon-technologies/valon-tools/apps/g-issues"},
		{name: "repository subdirectory", source: "github.com/valon-technologies/toolshed/valon-tools/apps/g-issues", want: "valon-tools/apps/g-issues"},
		{name: "not under apps", source: "github.com/valon-technologies/toolshed/valon-tools/g-issues", wantErr: true},
		{name: "nested app name", source: "github.com/valon-technologies/toolshed/apps/a/b", wantErr: true},
		{name: "parent traversal", source: "github.com/valon-technologies/toolshed/../apps/g-issues", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SourceDirFromManifestSource(tt.source)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SourceDirFromManifestSource(%q) error = %v, wantErr %v", tt.source, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("SourceDirFromManifestSource(%q) = %q, want %q", tt.source, got, tt.want)
			}
			if _, _, err := parseAppSource(tt.source); (err != nil) != tt.wantErr {
				t.Fatalf("parseAppSource(%q) error = %v, wantErr %v", tt.source, err, tt.wantErr)
			}
		})
	}
}
