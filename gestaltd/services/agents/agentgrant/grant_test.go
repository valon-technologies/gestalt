package agentgrant

import (
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func TestGrantToolRefsSetDistinguishesUnsetAndExplicitEmpty(t *testing.T) {
	t.Parallel()

	grants := newTestManager(t)
	unsetToken, err := grants.Mint(Grant{
		ProviderName: "test",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
	})
	if err != nil {
		t.Fatalf("Mint unset grant: %v", err)
	}
	unsetGrant, err := grants.Resolve(unsetToken)
	if err != nil {
		t.Fatalf("Resolve unset grant: %v", err)
	}
	if unsetGrant.ToolRefsSet {
		t.Fatalf("unset grant ToolRefsSet = true, want false")
	}

	emptyToken, err := grants.Mint(Grant{
		ProviderName: "test",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		ToolRefsSet:  true,
	})
	if err != nil {
		t.Fatalf("Mint explicit empty grant: %v", err)
	}
	emptyGrant, err := grants.Resolve(emptyToken)
	if err != nil {
		t.Fatalf("Resolve explicit empty grant: %v", err)
	}
	if !emptyGrant.ToolRefsSet || len(emptyGrant.ToolRefs) != 0 {
		t.Fatalf("explicit empty grant tool refs = set %t refs %#v, want set empty", emptyGrant.ToolRefsSet, emptyGrant.ToolRefs)
	}
}

func TestGrantToolRefsSetInferredForNonEmptyToolRefs(t *testing.T) {
	t.Parallel()

	grants := newTestManager(t)
	token, err := grants.Mint(Grant{
		ProviderName: "test",
		SessionID:    "session-1",
		TurnID:       "turn-1",
		SubjectID:    "user:user-123",
		ToolRefs: []coreagent.ToolRef{{
			App:       "linear",
			Operation: "searchIssues",
		}},
	})
	if err != nil {
		t.Fatalf("Mint grant: %v", err)
	}
	grant, err := grants.Resolve(token)
	if err != nil {
		t.Fatalf("Resolve grant: %v", err)
	}
	if !grant.ToolRefsSet || len(grant.ToolRefs) != 1 {
		t.Fatalf("non-empty grant tool refs = set %t refs %#v, want set with one ref", grant.ToolRefsSet, grant.ToolRefs)
	}
}

func newTestManager(t testing.TB) *Manager {
	t.Helper()
	grants, err := NewManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return grants
}
