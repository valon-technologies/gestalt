package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
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

func TestProviderServerStampsPublicRuntimeSourceLayer(t *testing.T) {
	t.Parallel()

	tuple := &proto.RelationshipTuple{
		Resource: &proto.Resource{Type: "app", Id: "demo"},
		Relation: "viewer",
		Target: &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
			},
		},
	}
	provider := &relationshipWriteRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)

	if _, err := server.AddRelationship(context.Background(), &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{Tuple: tuple},
	}); err != nil {
		t.Fatalf("internal AddRelationship: %v", err)
	}
	if got := provider.lastAdd.GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
		t.Fatalf("internal source layer = %v, want unspecified", got)
	}

	publicCtx := publicrpc.WithPublicOrigin(context.Background(), proto.Authorization_AddRelationship_FullMethodName)
	if _, err := server.AddRelationship(publicCtx, &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{Tuple: tuple},
	}); err != nil {
		t.Fatalf("public AddRelationship: %v", err)
	}
	if got := provider.lastAdd.GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
		t.Fatalf("public source layer = %v, want runtime", got)
	}

	if _, err := server.AddRelationship(publicCtx, &proto.AddRelationshipRequest{
		Relationship: &proto.Relationship{
			Tuple:       tuple,
			SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
		},
	}); err != nil {
		t.Fatalf("public static AddRelationship: %v", err)
	}
	if got := provider.lastAdd.GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG {
		t.Fatalf("explicit static source layer = %v, want static_config", got)
	}

	relationship := &proto.Relationship{Tuple: tuple}
	writeCtx := publicrpc.WithPublicOrigin(context.Background(), proto.Authorization_WriteRelationships_FullMethodName)
	if _, err := server.WriteRelationships(writeCtx, &proto.WriteRelationshipsRequest{
		Updates: []*proto.RelationshipUpdate{{
			Operation:    proto.RelationshipUpdate_OPERATION_TOUCH,
			Relationship: relationship,
		}},
	}); err != nil {
		t.Fatalf("public WriteRelationships touch: %v", err)
	}
	if got := provider.lastWrite.GetUpdates()[0].GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
		t.Fatalf("touch source layer = %v, want runtime", got)
	}

	if _, err := server.WriteRelationships(writeCtx, &proto.WriteRelationshipsRequest{
		Updates: []*proto.RelationshipUpdate{{
			Operation:    proto.RelationshipUpdate_OPERATION_DELETE,
			Relationship: &proto.Relationship{Tuple: tuple},
		}},
	}); err != nil {
		t.Fatalf("public WriteRelationships delete: %v", err)
	}
	if got := provider.lastWrite.GetUpdates()[0].GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
		t.Fatalf("delete source layer = %v, want unspecified", got)
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

type relationshipWriteRecordingAuthorizationProvider struct {
	core.AuthorizationProvider
	lastAdd   *proto.AddRelationshipRequest
	lastWrite *proto.WriteRelationshipsRequest
}

func (p *relationshipWriteRecordingAuthorizationProvider) AddRelationship(_ context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	p.lastAdd = req
	return &proto.AddRelationshipResponse{}, nil
}

func (p *relationshipWriteRecordingAuthorizationProvider) WriteRelationships(_ context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	p.lastWrite = req
	return &proto.WriteRelationshipsResponse{}, nil
}
