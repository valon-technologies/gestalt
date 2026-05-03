package bootstrap

import (
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/agents/agentgrant"
)

func newTestAgentRunGrants(t testing.TB) *agentgrant.Manager {
	t.Helper()
	grants, err := agentgrant.NewManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("agentgrant.NewManager: %v", err)
	}
	return grants
}

func mustMintAgentToolID(t testing.TB, grants *agentgrant.Manager, target coreagent.ToolTarget) string {
	t.Helper()
	id, err := grants.MintToolID(target)
	if err != nil {
		t.Fatalf("MintToolID: %v", err)
	}
	return id
}
