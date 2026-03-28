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
			input: "github.com/acme/gestalt-plugins/slack",
			want:  Source{Host: HostGitHub, Owner: "acme", Repo: "gestalt-plugins", Plugin: "slack"},
		},
		{
			name:  "valid with hyphens",
			input: "github.com/valon-technologies/toolshed/extend",
			want:  Source{Host: HostGitHub, Owner: "valon-technologies", Repo: "toolshed", Plugin: "extend"},
		},
		{
			name:  "valid with dots hyphens underscores",
			input: "github.com/my-org/my.repo/my_plugin",
			want:  Source{Host: HostGitHub, Owner: "my-org", Repo: "my.repo", Plugin: "my_plugin"},
		},
		{
			name:    "reject uppercase",
			input:   "github.com/Acme/plugins/slack",
			wantErr: true,
		},
		{
			name:    "reject missing plugin segment",
			input:   "github.com/acme/plugins",
			wantErr: true,
		},
		{
			name:    "reject extra segments",
			input:   "github.com/acme/plugins/slack/extra",
			wantErr: true,
		},
		{
			name:    "reject non-github host",
			input:   "gitlab.com/acme/plugins/slack",
			wantErr: true,
		},
		{
			name:    "reject empty segment",
			input:   "github.com//plugins/slack",
			wantErr: true,
		},
		{
			name:    "reject leading whitespace",
			input:   " github.com/acme/plugins/slack",
			wantErr: true,
		},
		{
			name:    "reject trailing whitespace",
			input:   "github.com/acme/plugins/slack ",
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

	const input = "github.com/acme/plugins/slack"
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

	src := Source{Host: HostGitHub, Owner: "acme", Repo: "plugins", Plugin: "slack"}
	const want = "gestalt-plugin-slack_v1.2.3.tar.gz"
	if got := src.AssetName("1.2.3"); got != want {
		t.Errorf("AssetName(1.2.3) = %q, want %q", got, want)
	}
}

func TestSourceReleaseTag(t *testing.T) {
	t.Parallel()

	src := Source{Host: HostGitHub, Owner: "acme", Repo: "plugins", Plugin: "slack"}
	const want = "v1.2.3"
	if got := src.ReleaseTag("1.2.3"); got != want {
		t.Errorf("ReleaseTag(1.2.3) = %q, want %q", got, want)
	}
}

func TestSourceRepoSlug(t *testing.T) {
	t.Parallel()

	src := Source{Host: HostGitHub, Owner: "acme", Repo: "plugins", Plugin: "slack"}
	const want = "acme/plugins"
	if got := src.RepoSlug(); got != want {
		t.Errorf("RepoSlug() = %q, want %q", got, want)
	}
}
