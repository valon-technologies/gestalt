package pluginsource

import (
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    Source
		wantErr bool
	}{
		{
			name:  "valid basic",
			input: "github.com/testowner/testrepo/testplugin",
			want:  Source{Host: HostGitHub, Owner: "testowner", Repo: "testrepo", Plugin: "testplugin"},
		},
		{
			name:  "valid with hyphens",
			input: "github.com/test-org/test-repo/test-plugin",
			want:  Source{Host: HostGitHub, Owner: "test-org", Repo: "test-repo", Plugin: "test-plugin"},
		},
		{
			name:  "valid with dots hyphens underscores",
			input: "github.com/my-org/my.repo/my_plugin",
			want:  Source{Host: HostGitHub, Owner: "my-org", Repo: "my.repo", Plugin: "my_plugin"},
		},
		{
			name:    "reject uppercase",
			input:   "github.com/TestOwner/testrepo/testplugin",
			wantErr: true,
		},
		{
			name:    "reject missing plugin segment",
			input:   "github.com/testowner/testrepo",
			wantErr: true,
		},
		{
			name:    "reject extra segments",
			input:   "github.com/testowner/testrepo/testplugin/extra",
			wantErr: true,
		},
		{
			name:    "reject non-github host",
			input:   "gitlab.com/testowner/testrepo/testplugin",
			wantErr: true,
		},
		{
			name:    "reject empty segment",
			input:   "github.com//testrepo/testplugin",
			wantErr: true,
		},
		{
			name:    "reject leading whitespace",
			input:   " github.com/testowner/testrepo/testplugin",
			wantErr: true,
		},
		{
			name:    "reject trailing whitespace",
			input:   "github.com/testowner/testrepo/testplugin ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) succeeded, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSourceStringRoundTrip(t *testing.T) {
	t.Parallel()

	const input = "github.com/testowner/testrepo/testplugin"
	src, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}
	if got := src.String(); got != input {
		t.Errorf("String() = %q, want %q", got, input)
	}
}

func TestSourceAssetName(t *testing.T) {
	t.Parallel()

	src := Source{Host: HostGitHub, Owner: "testowner", Repo: "testrepo", Plugin: "testplugin"}
	const want = "gestalt-plugin-testplugin_v1.2.3.tar.gz"
	if got := src.AssetName("1.2.3"); got != want {
		t.Errorf("AssetName(1.2.3) = %q, want %q", got, want)
	}
}

func TestSourceReleaseTag(t *testing.T) {
	t.Parallel()

	src := Source{Host: HostGitHub, Owner: "testowner", Repo: "testrepo", Plugin: "testplugin"}
	const want = "v1.2.3"
	if got := src.ReleaseTag("1.2.3"); got != want {
		t.Errorf("ReleaseTag(1.2.3) = %q, want %q", got, want)
	}
}

func TestSourceRepoSlug(t *testing.T) {
	t.Parallel()

	src := Source{Host: HostGitHub, Owner: "testowner", Repo: "testrepo", Plugin: "testplugin"}
	const want = "testowner/testrepo"
	if got := src.RepoSlug(); got != want {
		t.Errorf("RepoSlug() = %q, want %q", got, want)
	}
}
