package authorization

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestStampPublicRuntimeAddRelationship(t *testing.T) {
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
	provider := &sourceLayerRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)

	internalCtx := context.Background()
	if _, err := server.AddRelationship(internalCtx, &proto.AddRelationshipRequest{
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

	staticCtx := publicrpc.WithPublicOrigin(context.Background(), proto.Authorization_AddRelationship_FullMethodName)
	if _, err := server.AddRelationship(staticCtx, &proto.AddRelationshipRequest{
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
}

func TestStampPublicRuntimeWriteRelationships(t *testing.T) {
	t.Parallel()

	relationship := &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Resource: &proto.Resource{Type: "app", Id: "demo"},
			Relation: "viewer",
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{
					Subject: &proto.Subject{Type: "subject", Id: "user:viewer@example.com"},
				},
			},
		},
	}
	provider := &sourceLayerRecordingAuthorizationProvider{}
	server := NewProviderServer(provider)

	publicCtx := publicrpc.WithPublicOrigin(context.Background(), proto.Authorization_WriteRelationships_FullMethodName)
	if _, err := server.WriteRelationships(publicCtx, &proto.WriteRelationshipsRequest{
		Updates: []*proto.RelationshipUpdate{{
			Operation:    proto.RelationshipUpdate_OPERATION_TOUCH,
			Relationship: relationship,
		}},
	}); err != nil {
		t.Fatalf("public WriteRelationships: %v", err)
	}
	if got := provider.lastWrite.GetUpdates()[0].GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
		t.Fatalf("touch source layer = %v, want runtime", got)
	}

	if _, err := server.WriteRelationships(publicCtx, &proto.WriteRelationshipsRequest{
		Updates: []*proto.RelationshipUpdate{{
			Operation: proto.RelationshipUpdate_OPERATION_DELETE,
			Relationship: &proto.Relationship{
				Tuple: relationship.GetTuple(),
			},
		}},
	}); err != nil {
		t.Fatalf("public delete WriteRelationships: %v", err)
	}
	if got := provider.lastWrite.GetUpdates()[0].GetRelationship().GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED {
		t.Fatalf("delete source layer = %v, want unspecified", got)
	}
}

type sourceLayerRecordingAuthorizationProvider struct {
	core.AuthorizationProvider
	lastAdd   *proto.AddRelationshipRequest
	lastWrite *proto.WriteRelationshipsRequest
}

func (p *sourceLayerRecordingAuthorizationProvider) AddRelationship(_ context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	p.lastAdd = req
	return &proto.AddRelationshipResponse{}, nil
}

func (p *sourceLayerRecordingAuthorizationProvider) WriteRelationships(_ context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	p.lastWrite = req
	return &proto.WriteRelationshipsResponse{}, nil
}
