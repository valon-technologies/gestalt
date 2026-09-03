package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestProviderServerStampsGRPCEntry(t *testing.T) {
	t.Parallel()

	provider := &entryRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)

	_, err := server.CheckAccess(context.Background(), &proto.CheckAccessRequest{})
	if err != nil {
		t.Fatalf("CheckAccess: %v", err)
	}
	if provider.entry != invocation.EntryGRPC {
		t.Fatalf("entry = %q, want %q", provider.entry, invocation.EntryGRPC)
	}
}

func TestProviderServerDispatchesWriteRelationships(t *testing.T) {
	t.Parallel()

	provider := &entryRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)
	want := &proto.WriteRelationshipsRequest{}
	got, err := server.WriteRelationships(context.Background(), want)
	if err != nil {
		t.Fatalf("WriteRelationships: %v", err)
	}
	if got == nil || provider.request != want {
		t.Fatalf("forwarded request = %p, want exact request %p", provider.request, want)
	}
	if provider.entry != invocation.EntryGRPC {
		t.Fatalf("entry = %q, want %q", provider.entry, invocation.EntryGRPC)
	}
}

type entryRecordingAuthorizationProvider struct {
	core.AuthorizationProvider
	entry   invocation.Entry
	request *proto.WriteRelationshipsRequest
}

func (p *entryRecordingAuthorizationProvider) WriteRelationships(ctx context.Context, request *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	p.entry = invocation.EntryFromContext(ctx)
	p.request = request
	return &proto.WriteRelationshipsResponse{}, nil
}

func (p *entryRecordingAuthorizationProvider) CheckAccess(ctx context.Context, _ *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.entry = invocation.EntryFromContext(ctx)
	return &proto.CheckAccessResponse{Allowed: true}, nil
}
