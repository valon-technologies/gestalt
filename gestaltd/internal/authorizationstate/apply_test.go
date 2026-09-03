package authorizationstate

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type recordingProvider struct {
	relationships []*proto.Relationship
	resourceTypes []*proto.AuthorizationModelResourceType
	activeModel   *proto.AuthorizationModelRef
	addCalls      int
	setModelCalls int
}

func (p *recordingProvider) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{}, nil
}

func (p *recordingProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *recordingProvider) ListRelationships(_ context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if req.GetPageToken() != "" {
		return &proto.ListRelationshipsResponse{}, nil
	}
	return &proto.ListRelationshipsResponse{Relationships: append([]*proto.Relationship(nil), p.relationships...)}, nil
}

func (p *recordingProvider) AddRelationship(_ context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	p.addCalls++
	p.relationships = append(p.relationships, req.GetRelationship())
	return &proto.AddRelationshipResponse{Relationship: req.GetRelationship()}, nil
}

func (p *recordingProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *recordingProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (p *recordingProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{Model: p.activeModel}, nil
}

func (p *recordingProvider) SetActiveModel(_ context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	p.setModelCalls++
	p.activeModel = &proto.AuthorizationModelRef{
		Id:      req.GetModel().GetId(),
		Version: req.GetModel().GetVersion(),
	}
	return &proto.SetActiveModelResponse{Model: p.activeModel}, nil
}

func (p *recordingProvider) ListActiveModelResourceTypes(_ context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if req.GetPageToken() != "" {
		return &proto.ListActiveModelResourceTypesResponse{}, nil
	}
	return &proto.ListActiveModelResourceTypesResponse{
		ResourceTypes: append([]*proto.AuthorizationModelResourceType(nil), p.resourceTypes...),
	}, nil
}

func (p *recordingProvider) Ping(context.Context) error { return nil }
func (p *recordingProvider) Close() error               { return nil }

var _ core.AuthorizationProvider = (*recordingProvider)(nil)

func TestApplyIsIdempotentForExistingRelationships(t *testing.T) {
	t.Parallel()

	existing := &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Resource: &proto.Resource{Type: "app", Id: "home"},
			Relation: "admin",
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{
					Subject: &proto.Subject{Type: "subject", Id: "user:alice"},
				},
			},
		},
	}
	provider := &recordingProvider{relationships: []*proto.Relationship{existing}}
	req := &proto.SetAuthorizationStateRequest{
		Model: &proto.AuthorizationModel{
			ResourceTypes: []*proto.AuthorizationModelResourceType{{
				Name:        "app",
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			}},
		},
		Relationships: []*proto.Relationship{existing},
	}

	if _, err := Apply(context.Background(), provider, req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if provider.addCalls != 0 {
		t.Fatalf("add calls = %d, want 0 for existing relationship", provider.addCalls)
	}
	if provider.setModelCalls != 1 {
		t.Fatalf("set model calls = %d, want 1", provider.setModelCalls)
	}

	if _, err := Apply(context.Background(), provider, req); err != nil {
		t.Fatalf("Apply second time: %v", err)
	}
	if provider.addCalls != 0 {
		t.Fatalf("add calls after second apply = %d, want 0", provider.addCalls)
	}
}

func TestApplyAddsMissingRelationships(t *testing.T) {
	t.Parallel()

	provider := &recordingProvider{}
	req := &proto.SetAuthorizationStateRequest{
		Model: &proto.AuthorizationModel{
			ResourceTypes: []*proto.AuthorizationModelResourceType{{
				Name:        "app",
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			}},
		},
		Relationships: []*proto.Relationship{{
			Tuple: &proto.RelationshipTuple{
				Resource: &proto.Resource{Type: "app", Id: "home"},
				Relation: "viewer",
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_SubjectSet{
						SubjectSet: &proto.SubjectSet{
							Resource: &proto.Resource{Type: "group", Id: "valon-employees"},
							Relation: "member",
						},
					},
				},
			},
		}},
	}

	if _, err := Apply(context.Background(), provider, req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if provider.addCalls != 1 {
		t.Fatalf("add calls = %d, want 1", provider.addCalls)
	}
	if len(provider.relationships) != 1 {
		t.Fatalf("relationships = %d, want 1", len(provider.relationships))
	}
}
