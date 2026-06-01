package authorization

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestProviderBackedResolveAccessUsesModelPinnedRelationships(t *testing.T) {
	t.Parallel()

	provider := &providerBackedRoleContractProvider{
		activeModelID: "model-a",
		relationshipsByModelID: map[string][]*core.Relationship{
			"model-a": {
				providerBackedRoleTestRelationship("user:user-123", resourceTypeAppStatic, "slack", "editor"),
				providerBackedRoleTestRelationship("user:user-123", resourceTypeAppStatic, "slack", "viewer"),
			},
		},
	}
	authorizer := newProviderBackedRoleTestAuthorizer(t, provider, []string{"admin", "editor", "viewer"}, "model-a")

	access, allowed := authorizer.ResolveAccess(context.Background(), providerBackedRoleTestPrincipal(), "slack")
	if !allowed {
		t.Fatal("ResolveAccess denied, want allowed")
	}
	if access.Role != "editor" {
		t.Fatalf("resolved role = %q, want editor", access.Role)
	}
	if provider.checkAccessManyCalls != 0 {
		t.Fatalf("CheckAccessMany calls = %d, want 0", provider.checkAccessManyCalls)
	}
	if len(provider.readRequests) != 2 {
		t.Fatalf("ListRelationships calls = %d, want 2", len(provider.readRequests))
	}
	first := provider.readRequests[0]
	firstFilter := first.GetFilter()
	firstSubject := firstFilter.GetTarget().GetSubject()
	if got, want := firstSubject.GetType(), subjectTypeSubject; got != want {
		t.Fatalf("first subject type = %q, want %q", got, want)
	}
	if got, want := firstSubject.GetId(), "user:user-123"; got != want {
		t.Fatalf("first subject id = %q, want %q", got, want)
	}
	if got, want := firstFilter.GetResource().GetType(), resourceTypeAppStatic; got != want {
		t.Fatalf("first resource type = %q, want %q", got, want)
	}
	if got, want := firstFilter.GetResource().GetId(), "slack"; got != want {
		t.Fatalf("first resource id = %q, want %q", got, want)
	}
	if got, want := firstFilter.GetRelation(), "admin"; got != want {
		t.Fatalf("first relation = %q, want %q", got, want)
	}
	if got, want := first.GetPageSize(), int32(1); got != want {
		t.Fatalf("first page size = %d, want %d", got, want)
	}
	if got, want := provider.readRequests[1].GetFilter().GetRelation(), "editor"; got != want {
		t.Fatalf("second relation = %q, want %q", got, want)
	}
}

func TestProviderBackedResolveAccessDeniesMissingActiveRelationship(t *testing.T) {
	t.Parallel()

	provider := &providerBackedRoleContractProvider{
		activeModelID: "model-b",
		relationshipsByModelID: map[string][]*core.Relationship{
			"model-a": {
				providerBackedRoleTestRelationship("user:user-123", resourceTypeAppStatic, "slack", "admin"),
			},
		},
	}
	authorizer := newProviderBackedRoleTestAuthorizer(t, provider, []string{"admin"}, "model-a")

	_, allowed := authorizer.ResolveAccess(context.Background(), providerBackedRoleTestPrincipal(), "slack")
	if allowed {
		t.Fatal("ResolveAccess allowed relationship outside active provider state, want denial")
	}
	if provider.checkAccessManyCalls != 0 {
		t.Fatalf("CheckAccessMany calls = %d, want 0", provider.checkAccessManyCalls)
	}
}

func newProviderBackedRoleTestAuthorizer(t *testing.T, provider *providerBackedRoleContractProvider, roles []string, modelID string) *ProviderBackedAuthorizer {
	t.Helper()

	base, err := New(StaticConfig{
		Policies: map[string]StaticSubjectPolicy{
			"platform": {},
		},
		ProviderPolicies: map[string]string{
			"slack": "platform",
		},
	})
	if err != nil {
		t.Fatalf("New authorizer: %v", err)
	}
	authorizer, err := NewProviderBacked(base, provider)
	if err != nil {
		t.Fatalf("NewProviderBacked: %v", err)
	}
	authorizer.state = providerBackedRoleState{
		modelID: modelID,
		appStaticRoles: map[string][]string{
			"slack": roles,
		},
		appDynamicRoles:   map[string][]string{},
		policyStaticRoles: map[string][]string{},
	}
	return authorizer
}

func providerBackedRoleTestPrincipal() *principal.Principal {
	return &principal.Principal{
		UserID:    "user-123",
		SubjectID: principal.UserSubjectID("user-123"),
		Kind:      principal.KindUser,
	}
}

func providerBackedRoleTestRelationship(subjectID, resourceType, resourceID, relation string) *core.Relationship {
	return &core.Relationship{
		Tuple: &core.RelationshipTuple{
			Target: &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: &core.SubjectRef{
				Type: subjectTypeSubject,
				Id:   subjectID,
			}}},
			Relation: relation,
			Resource: &core.ResourceRef{Type: resourceType, Id: resourceID},
		},
	}
}

func mustStruct(t *testing.T, fields map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return out
}

func authorizationModelResourceType(model *core.AuthorizationModel, name string) *core.AuthorizationModelResourceType {
	for _, resourceType := range model.GetResourceTypes() {
		if resourceType.GetName() == name {
			return resourceType
		}
	}
	return nil
}

type providerBackedComposerTestProvider struct {
	activeModelID          string
	lastModel              *core.AuthorizationModel
	nextModel              int
	relationshipsByModelID map[string]map[string]*core.Relationship
}

func newProviderBackedComposerTestProvider(activeModelID string, relationships ...*core.Relationship) *providerBackedComposerTestProvider {
	p := &providerBackedComposerTestProvider{
		activeModelID:          activeModelID,
		relationshipsByModelID: map[string]map[string]*core.Relationship{},
	}
	if activeModelID != "" {
		p.relationshipsByModelID[activeModelID] = map[string]*core.Relationship{}
		for _, rel := range relationships {
			p.relationshipsByModelID[activeModelID][relationshipMapKey(rel)] = rel
		}
	}
	return p
}

func (p *providerBackedComposerTestProvider) Name() string { return "composer-test" }

func (p *providerBackedComposerTestProvider) CheckAccess(context.Context, *core.CheckAccessRequest) (*core.CheckAccessResponse, error) {
	return &core.CheckAccessResponse{}, nil
}

func (p *providerBackedComposerTestProvider) CheckAccessMany(context.Context, *core.CheckAccessManyRequest) (*core.CheckAccessManyResponse, error) {
	return &core.CheckAccessManyResponse{}, nil
}

func (p *providerBackedComposerTestProvider) ListRelationships(_ context.Context, req *core.ListRelationshipsRequest) (*core.ListRelationshipsResponse, error) {
	modelID := p.activeModelID
	out := []*core.Relationship{}
	for _, rel := range p.relationshipsByModelID[modelID] {
		if !providerBackedRoleRelationshipMatches(rel, req) {
			continue
		}
		out = append(out, rel)
	}
	return &core.ListRelationshipsResponse{Relationships: out}, nil
}

func (p *providerBackedComposerTestProvider) AddRelationship(_ context.Context, req *core.AddRelationshipRequest) (*core.AddRelationshipResponse, error) {
	modelID := p.activeModelID
	if p.relationshipsByModelID[modelID] == nil {
		p.relationshipsByModelID[modelID] = map[string]*core.Relationship{}
	}
	rel := req.GetRelationship()
	p.relationshipsByModelID[modelID][relationshipMapKey(rel)] = rel
	return &core.AddRelationshipResponse{Relationship: rel}, nil
}

func (p *providerBackedComposerTestProvider) DeleteRelationship(_ context.Context, req *core.DeleteRelationshipRequest) (*core.DeleteRelationshipResponse, error) {
	delete(p.relationshipsByModelID[p.activeModelID], relationshipTupleMapKey(req.GetRelationshipTuple()))
	return &core.DeleteRelationshipResponse{}, nil
}

func (p *providerBackedComposerTestProvider) SetRelationships(_ context.Context, req *core.SetRelationshipsRequest) (*core.SetRelationshipsResponse, error) {
	p.relationshipsByModelID[p.activeModelID] = map[string]*core.Relationship{}
	for _, rel := range req.GetRelationships() {
		p.relationshipsByModelID[p.activeModelID][relationshipMapKey(rel)] = rel
	}
	return &core.SetRelationshipsResponse{Relationships: req.GetRelationships()}, nil
}

func (p *providerBackedComposerTestProvider) GetActiveModelRef(context.Context) (*core.GetActiveModelRefResponse, error) {
	return &core.GetActiveModelRefResponse{Model: &core.AuthorizationModelRef{Id: p.activeModelID}}, nil
}

func (p *providerBackedComposerTestProvider) SetActiveModel(_ context.Context, req *core.SetActiveModelRequest) (*core.SetActiveModelResponse, error) {
	p.nextModel++
	modelID := fmt.Sprintf("model-%d", p.nextModel)
	if p.activeModelID != "" {
		p.relationshipsByModelID[modelID] = map[string]*core.Relationship{}
		for key, rel := range p.relationshipsByModelID[p.activeModelID] {
			p.relationshipsByModelID[modelID][key] = rel
		}
	}
	p.activeModelID = modelID
	p.lastModel = req.GetModel()
	return &core.SetActiveModelResponse{Model: &core.AuthorizationModelRef{Id: modelID}}, nil
}

func (p *providerBackedComposerTestProvider) ListActiveModelResourceTypes(context.Context, *core.ListActiveModelResourceTypesRequest) (*core.ListActiveModelResourceTypesResponse, error) {
	return &core.ListActiveModelResourceTypesResponse{}, nil
}

func (p *providerBackedComposerTestProvider) hasRelationship(rel *core.Relationship) bool {
	return p.relationshipsByModelID[p.activeModelID][relationshipMapKey(rel)] != nil
}

type providerBackedRoleContractProvider struct {
	activeModelID          string
	relationshipsByModelID map[string][]*core.Relationship
	readRequests           []*core.ListRelationshipsRequest
	checkAccessManyCalls   int
}

func (p *providerBackedRoleContractProvider) Name() string { return "role-contract" }

func (p *providerBackedRoleContractProvider) CheckAccess(context.Context, *core.CheckAccessRequest) (*core.CheckAccessResponse, error) {
	return &core.CheckAccessResponse{}, nil
}

func (p *providerBackedRoleContractProvider) CheckAccessMany(context.Context, *core.CheckAccessManyRequest) (*core.CheckAccessManyResponse, error) {
	p.checkAccessManyCalls++
	return &core.CheckAccessManyResponse{}, nil
}

func (p *providerBackedRoleContractProvider) ListRelationships(_ context.Context, req *core.ListRelationshipsRequest) (*core.ListRelationshipsResponse, error) {
	p.readRequests = append(p.readRequests, req)
	modelID := p.activeModelID
	out := make([]*core.Relationship, 0)
	for _, rel := range p.relationshipsByModelID[modelID] {
		if !providerBackedRoleRelationshipMatches(rel, req) {
			continue
		}
		out = append(out, rel)
		if req.GetPageSize() > 0 && int32(len(out)) >= req.GetPageSize() {
			break
		}
	}
	return &core.ListRelationshipsResponse{Relationships: out}, nil
}

func (p *providerBackedRoleContractProvider) AddRelationship(context.Context, *core.AddRelationshipRequest) (*core.AddRelationshipResponse, error) {
	return nil, errors.New("AddRelationship not implemented")
}

func (p *providerBackedRoleContractProvider) DeleteRelationship(context.Context, *core.DeleteRelationshipRequest) (*core.DeleteRelationshipResponse, error) {
	return nil, errors.New("DeleteRelationship not implemented")
}

func (p *providerBackedRoleContractProvider) SetRelationships(context.Context, *core.SetRelationshipsRequest) (*core.SetRelationshipsResponse, error) {
	return nil, errors.New("SetRelationships not implemented")
}

func (p *providerBackedRoleContractProvider) GetActiveModelRef(context.Context) (*core.GetActiveModelRefResponse, error) {
	return &core.GetActiveModelRefResponse{Model: &core.AuthorizationModelRef{Id: p.activeModelID}}, nil
}

func (p *providerBackedRoleContractProvider) SetActiveModel(context.Context, *core.SetActiveModelRequest) (*core.SetActiveModelResponse, error) {
	return nil, errors.New("SetActiveModel not implemented")
}

func (p *providerBackedRoleContractProvider) ListActiveModelResourceTypes(context.Context, *core.ListActiveModelResourceTypesRequest) (*core.ListActiveModelResourceTypesResponse, error) {
	return &core.ListActiveModelResourceTypesResponse{}, nil
}

func providerBackedRoleRelationshipMatches(rel *core.Relationship, req *core.ListRelationshipsRequest) bool {
	if rel == nil {
		return false
	}
	filter := req.GetFilter()
	if target := filter.GetTarget(); target != nil {
		if RelationshipTargetMapKey(relationshipTarget(rel), nil) != RelationshipTargetMapKey(target, nil) {
			return false
		}
	}
	if resource := filter.GetResource(); resource != nil {
		if relationshipResource(rel).GetType() != resource.GetType() || relationshipResource(rel).GetId() != resource.GetId() {
			return false
		}
	}
	if relation := filter.GetRelation(); relation != "" && relationshipRelation(rel) != relation {
		return false
	}
	return true
}
