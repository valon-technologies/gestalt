package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestProviderServerStampsAppSDKOrigin(t *testing.T) {
	t.Parallel()

	provider := &originRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)

	_, err := server.CheckAccess(context.Background(), &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if provider.origin != invocation.OriginAppSDK {
		t.Fatalf("origin = %q, want %q", provider.origin, invocation.OriginAppSDK)
	}
}

type originRecordingAuthorizationProvider struct {
	core.AuthorizationProvider
	origin invocation.Origin
}

func (p *originRecordingAuthorizationProvider) CheckAccess(ctx context.Context, _ *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.origin = invocation.OriginFromContext(ctx)
	return &proto.CheckAccessResponse{Allowed: true}, nil
}
