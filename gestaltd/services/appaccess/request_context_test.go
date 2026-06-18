package appaccess

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestProviderRequestContextRestoreDoesNotTrustAgentInternalConnectionAccess(t *testing.T) {
	t.Parallel()

	reqCtx := &proto.RequestContext{
		Caller: &proto.ProviderContext{
			Kind: string(invocation.ProviderKindApp),
			Name: "alpha",
		},
		Subject: &proto.SubjectContext{
			Id: "user:runner",
		},
		Agent: &proto.AgentInvocationContext{
			ProviderName: "alpha",
			SessionId:    "session-1",
			TurnId:       "turn-1",
		},
		Invocation: &proto.InvocationContext{
			InternalConnectionAccess: true,
		},
	}
	providerCtx, err := ProviderRequestContextFromProto(reqCtx, "", "")
	if err != nil {
		t.Fatalf("ProviderRequestContextFromProto: %v", err)
	}
	ctx := providerCtx.Restore(context.Background(), "")
	if invocation.InternalConnectionAccessFromContext(ctx) {
		t.Fatal("internal connection access restored for agent context")
	}
	if got := invocation.AgentInvocationContextFromContext(ctx); got.ProviderName != "alpha" || got.SessionID != "session-1" || got.TurnID != "turn-1" {
		t.Fatalf("agent context = %#v, want alpha/session-1/turn-1", got)
	}
}
