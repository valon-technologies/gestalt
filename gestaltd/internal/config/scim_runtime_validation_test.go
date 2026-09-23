package config_test

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func testSCIMModelProvider() *scimRuntimeModelProvider {
	return &scimRuntimeModelProvider{resourceTypes: []*proto.AuthorizationModelResourceType{{
		Name: "group",
		Relations: []*proto.ModelRelation{{
			Name: "member",
			AllowedTargets: []*proto.ModelAllowedTarget{
				{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"}},
				{Kind: &proto.ModelAllowedTarget_SubjectSetType{SubjectSetType: &proto.SubjectSetType{ResourceType: "group", Relation: "member"}}},
			},
		}},
	}}}
}

func TestValidateRuntimeSCIMConfigAcceptsValidClientWithoutTokens(t *testing.T) {
	t.Parallel()
	cfg := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{
		"rippling": {
			AuthoritativeUserDomains: []string{"valon.com"},
			ActiveUserRelationships: []config.SCIMRelationshipConfig{{
				Relation: "member",
				Resource: config.AuthorizationResourceDef{Type: "group", ID: "employees"},
			}},
		},
	}}
	if err := config.ValidateRuntimeSCIMConfig(context.Background(), cfg, testSCIMModelProvider()); err != nil {
		t.Fatalf("ValidateRuntimeSCIMConfig: %v", err)
	}
}

func TestValidateRuntimeSCIMConfigRejectsOverlappingProjectionsAndDomains(t *testing.T) {
	t.Parallel()
	cfg := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{
		"one": {
			AuthoritativeUserDomains: []string{"valon.com"},
			ActiveUserRelationships: []config.SCIMRelationshipConfig{{
				Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "employees"},
			}},
		},
		"two": {
			AuthoritativeUserDomains: []string{"valon.com"},
			ActiveUserRelationships: []config.SCIMRelationshipConfig{{
				Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "employees"},
			}},
		},
	}}
	if err := config.ValidateRuntimeSCIMConfig(context.Background(), cfg, testSCIMModelProvider()); err == nil {
		t.Fatal("expected overlapping domain and projection to be rejected")
	}
}

func TestValidateRuntimeSCIMConfigRejectsUnknownRelationAndMissingProvider(t *testing.T) {
	t.Parallel()
	cfg := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{
		"rippling": {
			ActiveUserRelationships: []config.SCIMRelationshipConfig{{
				Relation: "admin", Resource: config.AuthorizationResourceDef{Type: "group", ID: "employees"},
			}},
		},
	}}
	if err := config.ValidateRuntimeSCIMConfig(context.Background(), cfg, testSCIMModelProvider()); err == nil {
		t.Fatal("expected unknown relation to be rejected")
	}
	valid := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{
		"rippling": {ActiveUserRelationships: []config.SCIMRelationshipConfig{{
			Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "employees"},
		}}},
	}}
	if err := config.ValidateRuntimeSCIMConfig(context.Background(), valid, nil); err == nil {
		t.Fatal("expected missing authorization provider to be rejected")
	}
}

type scimRuntimeModelProvider struct {
	resourceTypes []*proto.AuthorizationModelResourceType
}

func (p *scimRuntimeModelProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: p.resourceTypes}, nil
}
