package gestalt

import "testing"

func TestCloneAgentWorkspaceNil(t *testing.T) {
	t.Parallel()
	if got := CloneAgentWorkspace(nil); got != nil {
		t.Fatalf("CloneAgentWorkspace(nil) = %#v, want nil", got)
	}
}

func TestCloneAgentWorkspaceIsolatesCheckouts(t *testing.T) {
	t.Parallel()

	src := &AgentWorkspace{
		CWD: "toolshed",
		Checkouts: []AgentWorkspaceGitCheckout{{
			URL:  "https://github.com/valon-technologies/toolshed.git",
			Ref:  "main",
			Path: "toolshed",
		}},
	}
	cloned := CloneAgentWorkspace(src)
	if cloned == nil || cloned.CWD != "toolshed" || len(cloned.Checkouts) != 1 {
		t.Fatalf("cloned workspace = %#v, want cwd toolshed and one checkout", cloned)
	}
	src.Checkouts = append(src.Checkouts, AgentWorkspaceGitCheckout{Path: "gestalt"})
	if len(cloned.Checkouts) != 1 {
		t.Fatalf("cloned checkouts = %#v, want isolated from src mutation", cloned.Checkouts)
	}
}
