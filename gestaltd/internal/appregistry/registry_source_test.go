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
