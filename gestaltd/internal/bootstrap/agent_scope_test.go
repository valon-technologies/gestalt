package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/services/agents/agenttoolid"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
)

func newTestAgentToolIDs(t testing.TB) *agenttoolid.Codec {
	t.Helper()
	codec, err := agenttoolid.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("agenttoolid.NewCodec: %v", err)
	}
	return codec
}

func newTestAgentTurnScopes() *agentturnscope.Store {
	return agentturnscope.NewStore()
}
