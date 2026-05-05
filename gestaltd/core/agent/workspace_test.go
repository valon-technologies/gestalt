package agent

import (
	"strings"
	"testing"
)

func TestCanonicalGitRepositoryIdentityRejectsSchemeLessPaths(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"repo", "org/repo", "github.com/org/repo"} {
		_, err := CanonicalGitRepositoryIdentity(raw)
		if err == nil || !strings.Contains(err.Error(), "scheme is required") {
			t.Fatalf("CanonicalGitRepositoryIdentity(%q) error = %v, want scheme required", raw, err)
		}
	}
}

func TestCanonicalGitRepositoryIdentityRejectsLocalPaths(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"./repo", "../repo", "/tmp/repo"} {
		_, err := CanonicalGitRepositoryIdentity(raw)
		if err == nil || !strings.Contains(err.Error(), "local git paths are not allowed") {
			t.Fatalf("CanonicalGitRepositoryIdentity(%q) error = %v, want local path rejection", raw, err)
		}
	}
}

func TestCanonicalGitRepositoryIdentityAllowsCloneURLs(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"git@github.com:valon-technologies/app.git":     "github.com/valon-technologies/app",
		"https://github.com/valon-technologies/app.git": "github.com/valon-technologies/app",
	}
	for raw, want := range tests {
		got, err := CanonicalGitRepositoryIdentity(raw)
		if err != nil {
			t.Fatalf("CanonicalGitRepositoryIdentity(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("CanonicalGitRepositoryIdentity(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCanonicalGitRepositoryAllowlistIdentityAllowsCanonicalNames(t *testing.T) {
	t.Parallel()

	got, err := CanonicalGitRepositoryAllowlistIdentity("github.com/valon-technologies/app.git")
	if err != nil {
		t.Fatalf("CanonicalGitRepositoryAllowlistIdentity: %v", err)
	}
	if got != "github.com/valon-technologies/app" {
		t.Fatalf("CanonicalGitRepositoryAllowlistIdentity = %q, want canonical repository identity", got)
	}
}

func TestNormalizeWorkspaceRejectsSchemeLessCheckoutURL(t *testing.T) {
	t.Parallel()

	_, err := NormalizeWorkspace(&Workspace{
		CWD: "app",
		Checkouts: []WorkspaceGitCheckout{{
			URL:  "github.com/valon-technologies/app",
			Path: "app",
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "scheme is required") {
		t.Fatalf("NormalizeWorkspace error = %v, want scheme required", err)
	}
}
