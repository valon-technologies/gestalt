package config

import "testing"

func TestParseGitHubRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    GitHubRepo
		wantErr bool
	}{
		{
			name: "https with git suffix",
			raw:  "https://github.com/example/apps.git",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "http",
			raw:  "http://github.com/example/apps",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "www host",
			raw:  "https://www.github.com/example/apps.git",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "ssh scp",
			raw:  "git@github.com:example/apps.git",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "ssh url",
			raw:  "ssh://git@github.com/example/apps.git",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "preserves owner case",
			raw:  "https://github.com/Valon-Technologies/Gestalt-Providers.git",
			want: GitHubRepo{Owner: "Valon-Technologies", Name: "Gestalt-Providers"},
		},
		{
			name:    "gitlab omitted",
			raw:     "https://gitlab.example.com/group/app.git",
			wantErr: true,
		},
		{
			name:    "empty",
			raw:     "  ",
			wantErr: true,
		},
		{
			name:    "missing name",
			raw:     "https://github.com/example",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGitHubRepo(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseGitHubRepo(%q) = %+v, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGitHubRepo(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("ParseGitHubRepo(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseGitHubSnapshotRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    GitHubRepo
		wantErr bool
	}{
		{
			name: "https",
			raw:  "https://github.com/example/apps.git",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "ssh scp",
			raw:  "git@github.com:example/apps.git",
			want: GitHubRepo{Owner: "example", Name: "apps"},
		},
		{
			name: "preserves owner case",
			raw:  "https://github.com/Valon-Technologies/Gestalt-Providers.git",
			want: GitHubRepo{Owner: "Valon-Technologies", Name: "Gestalt-Providers"},
		},
		{
			name:    "http rejected",
			raw:     "http://github.com/example/apps.git",
			wantErr: true,
		},
		{
			name:    "ssh url rejected",
			raw:     "ssh://git@github.com/example/apps.git",
			wantErr: true,
		},
		{
			name:    "www host rejected",
			raw:     "https://www.github.com/example/apps.git",
			wantErr: true,
		},
		{
			name:    "gitlab rejected",
			raw:     "https://gitlab.example.com/group/app.git",
			wantErr: true,
		},
		{
			name:    "empty",
			raw:     "  ",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGitHubSnapshotRemote(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseGitHubSnapshotRemote(%q) = %+v, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGitHubSnapshotRemote(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("ParseGitHubSnapshotRemote(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}
