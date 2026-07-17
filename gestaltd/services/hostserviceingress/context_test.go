package hostserviceingress

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/metadata"
)

func TestApplyCapabilityRestoresPrincipal(t *testing.T) {
	t.Parallel()

	ctx := ApplyCapability(context.Background(), runtimehost.HostServiceRelayTarget{
		AppName:   "demo-app",
		SessionID: "session-1",
		Caller: &runtimehost.PrincipalClaims{
			SubjectID: "user:alice",
			Scopes:    []string{"openid"},
			ClientID:  "client-1",
		},
	})
	got := principal.FromContext(ctx)
	if got == nil || got.SubjectID != "user:alice" {
		t.Fatalf("principal.FromContext() = %#v, want user:alice", got)
	}
}

func TestApplyCapabilityIgnoresForgedIncomingMetadata(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-gestalt-caller-proof-subject", "user:bob",
	))
	ctx = ApplyCapability(ctx, runtimehost.HostServiceRelayTarget{
		Caller: &runtimehost.PrincipalClaims{SubjectID: "user:alice"},
	})
	got := principal.FromContext(ctx)
	if got == nil || got.SubjectID != "user:alice" {
		t.Fatalf("principal.FromContext() = %#v, want user:alice", got)
	}
}

func TestApplyCapabilityAllowsCallerDependentMethodAfterIngress(t *testing.T) {
	t.Parallel()

	manager, err := runtimehost.NewHostServiceRelayTokenManager([]byte("hostserviceingress-test-secret-01"))
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	manager.SetCapabilityIngressDecorator(ApplyCapability)
	method := "/" + proto.App_ServiceDesc.ServiceName + "/Invoke"
	token, err := manager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		AppName:      "demo-app",
		Service:      "app",
		MethodPrefix: "/" + proto.App_ServiceDesc.ServiceName + "/",
		Caller:       &runtimehost.PrincipalClaims{SubjectID: "user:alice"},
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		runtimehost.HostServiceRelayTokenHeader, token,
	))
	ctx, err = manager.AuthenticateGRPC(ctx, method)
	if err != nil {
		t.Fatalf("AuthenticateGRPC: %v", err)
	}
	got := principal.FromContext(ctx)
	if got == nil || got.SubjectID != "user:alice" {
		t.Fatalf("principal.FromContext() = %#v, want user:alice", got)
	}
}
