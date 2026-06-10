package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

func TestProviderServerLeavesGatewaySourceInternalByDefault(t *testing.T) {
	provider := &sourceRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)

	_, err := server.CheckAccess(context.Background(), &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if provider.source != providergateway.GatewaySourceInternal {
		t.Fatalf("source = %q, want %q", provider.source, providergateway.GatewaySourceInternal)
	}
}

func TestProviderServerAppliesConfiguredGatewaySource(t *testing.T) {
	provider := &sourceRecordingAuthorizationProvider{}
	server := NewProviderServer(provider, WithGatewaySource(providergateway.GatewaySourceSDKGRPC))

	_, err := server.CheckAccess(context.Background(), &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if provider.source != providergateway.GatewaySourceSDKGRPC {
		t.Fatalf("source = %q, want %q", provider.source, providergateway.GatewaySourceSDKGRPC)
	}
}

type sourceRecordingAuthorizationProvider struct {
	core.AuthorizationProvider
	source providergateway.GatewaySource
}

func (p *sourceRecordingAuthorizationProvider) CheckAccess(ctx context.Context, _ *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.source = providergateway.SourceFromContext(ctx)
	return &proto.CheckAccessResponse{Allowed: true}, nil
}
