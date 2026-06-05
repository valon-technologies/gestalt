package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/access"
	"github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type testAuthorizationProvider struct {
	allowed                    bool
	checkAccessRequests        []*proto.CheckAccessRequest
	setAuthorizationStateCalls int
	core.AuthorizationProvider
}

func (p *testAuthorizationProvider) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.checkAccessRequests = append(p.checkAccessRequests, req)
	return &proto.CheckAccessResponse{Allowed: p.allowed}, nil
}

func (p *testAuthorizationProvider) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	p.setAuthorizationStateCalls++
	return &proto.SetAuthorizationStateResponse{}, nil
}

func TestHostServerMutationDeniedByAccess(t *testing.T) {
	t.Parallel()

	provider := &testAuthorizationProvider{}
	policy := &testAuthorizationProvider{allowed: false}
	server := NewHostServer(provider, access.NewEnforcer(policy), nil)
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:123"})

	_, err := server.SetAuthorizationState(ctx, &proto.SetAuthorizationStateRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("SetAuthorizationState code = %v, want %v (err %v)", status.Code(err), codes.PermissionDenied, err)
	}
	if provider.setAuthorizationStateCalls != 0 {
		t.Fatalf("SetAuthorizationState forwarded calls = %d, want 0", provider.setAuthorizationStateCalls)
	}
	if len(policy.checkAccessRequests) != 1 {
		t.Fatalf("CheckAccess calls = %d, want 1", len(policy.checkAccessRequests))
	}
	if got := policy.checkAccessRequests[0].Action.Name; got != "SetAuthorizationState" {
		t.Fatalf("CheckAccess action = %q", got)
	}
}

func TestHostServerMutationUsesInvocationTokenPrincipal(t *testing.T) {
	t.Parallel()

	provider := &testAuthorizationProvider{}
	policy := &testAuthorizationProvider{allowed: true}
	tokens, err := appaccess.NewInvocationTokenManager([]byte("authorization-host-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	tokenCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:123"})
	token, err := tokens.MintRootToken(tokenCtx, "caller-app", nil)
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(appaccess.InvocationTokenMetadataKey, token))
	server := NewHostServer(provider, access.NewEnforcer(policy), tokens)

	if _, err := server.SetAuthorizationState(ctx, &proto.SetAuthorizationStateRequest{}); err != nil {
		t.Fatalf("SetAuthorizationState error = %v", err)
	}
	if provider.setAuthorizationStateCalls != 1 {
		t.Fatalf("SetAuthorizationState forwarded calls = %d, want 1", provider.setAuthorizationStateCalls)
	}
	if len(policy.checkAccessRequests) != 1 {
		t.Fatalf("CheckAccess calls = %d, want 1", len(policy.checkAccessRequests))
	}
	req := policy.checkAccessRequests[0]
	if req.Subject.Id != "user:123" {
		t.Fatalf("CheckAccess subject = %q", req.Subject.Id)
	}
	if req.Resource.Type != "AuthorizationProvider" || req.Action.Name != "SetAuthorizationState" {
		t.Fatalf("CheckAccess resource/action = %#v %#v", req.Resource, req.Action)
	}
}
