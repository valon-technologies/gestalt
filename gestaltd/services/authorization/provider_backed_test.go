package authorization

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestExternalIdentityAssumptionSubjectRefsIncludesLegacyUserSubjectType(t *testing.T) {
	t.Parallel()

	refs := externalIdentityAssumptionSubjectRefs(&principal.Principal{
		UserID:    "user-123",
		SubjectID: principal.UserSubjectID("user-123"),
		Kind:      principal.KindUser,
	})
	if len(refs) != 2 {
		t.Fatalf("subject refs length = %d, want 2: %#v", len(refs), refs)
	}
	if refs[0].GetType() != subjectTypeSubject || refs[0].GetId() != "user:user-123" {
		t.Fatalf("subject ref[0] = %#v", refs[0])
	}
	if refs[1].GetType() != subjectTypeUser || refs[1].GetId() != "user:user-123" {
		t.Fatalf("subject ref[1] = %#v", refs[1])
	}
}

func TestProviderBackedResolveAccessUsesModelPinnedRelationships(t *testing.T) {
	t.Parallel()

	provider := &providerBackedRoleContractProvider{
		activeModelID: "model-b",
		relationshipsByModelID: map[string][]*core.Relationship{
			"model-a": {
				providerBackedRoleTestRelationship("user:user-123", resourceTypePluginStatic, "slack", "editor"),
				providerBackedRoleTestRelationship("user:user-123", resourceTypePluginStatic, "slack", "viewer"),
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
	if provider.evaluateManyCalls != 0 {
		t.Fatalf("EvaluateMany calls = %d, want 0", provider.evaluateManyCalls)
	}
	if len(provider.readRequests) != 2 {
		t.Fatalf("ReadRelationships calls = %d, want 2", len(provider.readRequests))
	}
	first := provider.readRequests[0]
	if got, want := first.GetSubject().GetType(), subjectTypeSubject; got != want {
		t.Fatalf("first subject type = %q, want %q", got, want)
	}
	if got, want := first.GetSubject().GetId(), "user:user-123"; got != want {
		t.Fatalf("first subject id = %q, want %q", got, want)
	}
	if got, want := first.GetResource().GetType(), resourceTypePluginStatic; got != want {
		t.Fatalf("first resource type = %q, want %q", got, want)
	}
	if got, want := first.GetResource().GetId(), "slack"; got != want {
		t.Fatalf("first resource id = %q, want %q", got, want)
	}
	if got, want := first.GetRelation(), "admin"; got != want {
		t.Fatalf("first relation = %q, want %q", got, want)
	}
	if got, want := first.GetPageSize(), int32(1); got != want {
		t.Fatalf("first page size = %d, want %d", got, want)
	}
	if got, want := first.GetModelId(), "model-a"; got != want {
		t.Fatalf("first model id = %q, want %q", got, want)
	}
	if got, want := provider.readRequests[1].GetRelation(), "editor"; got != want {
		t.Fatalf("second relation = %q, want %q", got, want)
	}
}

func TestProviderBackedResolveAccessDeniesMissingCachedModelRelationship(t *testing.T) {
	t.Parallel()

	provider := &providerBackedRoleContractProvider{
		activeModelID: "model-b",
		relationshipsByModelID: map[string][]*core.Relationship{
			"model-b": {
				providerBackedRoleTestRelationship("user:user-123", resourceTypePluginStatic, "slack", "admin"),
			},
		},
	}
	authorizer := newProviderBackedRoleTestAuthorizer(t, provider, []string{"admin"}, "model-a")

	_, allowed := authorizer.ResolveAccess(context.Background(), providerBackedRoleTestPrincipal(), "slack")
	if allowed {
		t.Fatal("ResolveAccess allowed relationship from active model, want denial against cached model")
	}
	if provider.evaluateManyCalls != 0 {
		t.Fatalf("EvaluateMany calls = %d, want 0", provider.evaluateManyCalls)
	}
}

func TestProviderBackedResolveAccessDeniesMismatchedReadRelationshipsModel(t *testing.T) {
	t.Parallel()

	provider := &providerBackedRoleContractProvider{
		responseModelID: "model-b",
		relationshipsByModelID: map[string][]*core.Relationship{
			"model-a": {
				providerBackedRoleTestRelationship("user:user-123", resourceTypePluginStatic, "slack", "admin"),
			},
		},
	}
	authorizer := newProviderBackedRoleTestAuthorizer(t, provider, []string{"admin"}, "model-a")

	_, allowed := authorizer.ResolveAccess(context.Background(), providerBackedRoleTestPrincipal(), "slack")
	if allowed {
		t.Fatal("ResolveAccess allowed mismatched response model, want fail-closed denial")
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
		pluginStaticRoles: map[string][]string{
			"slack": roles,
		},
		pluginDynamicRoles: map[string][]string{},
		policyStaticRoles:  map[string][]string{},
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
		Subject:  &core.SubjectRef{Type: subjectTypeSubject, Id: subjectID},
		Relation: relation,
		Resource: &core.ResourceRef{Type: resourceType, Id: resourceID},
	}
}

type providerBackedRoleContractProvider struct {
	activeModelID          string
	responseModelID        string
	relationshipsByModelID map[string][]*core.Relationship
	readRequests           []*core.ReadRelationshipsRequest
	evaluateManyCalls      int
}

func (p *providerBackedRoleContractProvider) Name() string { return "role-contract" }

func (p *providerBackedRoleContractProvider) Evaluate(ctx context.Context, req *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	resp, err := p.EvaluateMany(ctx, &core.AccessEvaluationsRequest{Requests: []*core.AccessEvaluationRequest{req}})
	if err != nil {
		return nil, err
	}
	if len(resp.GetDecisions()) == 0 {
		return &core.AccessDecision{}, nil
	}
	return resp.GetDecisions()[0], nil
}

func (p *providerBackedRoleContractProvider) EvaluateMany(context.Context, *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	p.evaluateManyCalls++
	return &core.AccessEvaluationsResponse{}, nil
}

func (p *providerBackedRoleContractProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return nil, errors.New("SearchResources not implemented")
}

func (p *providerBackedRoleContractProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return nil, errors.New("SearchSubjects not implemented")
}

func (p *providerBackedRoleContractProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return nil, errors.New("SearchActions not implemented")
}

func (p *providerBackedRoleContractProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}

func (p *providerBackedRoleContractProvider) ReadRelationships(_ context.Context, req *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	p.readRequests = append(p.readRequests, req)
	modelID := req.GetModelId()
	if modelID == "" {
		modelID = p.activeModelID
	}
	respModelID := modelID
	if p.responseModelID != "" {
		respModelID = p.responseModelID
	}
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
	return &core.ReadRelationshipsResponse{Relationships: out, ModelId: respModelID}, nil
}

func (p *providerBackedRoleContractProvider) WriteRelationships(context.Context, *core.WriteRelationshipsRequest) error {
	return errors.New("WriteRelationships not implemented")
}

func (p *providerBackedRoleContractProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{Model: &core.AuthorizationModelRef{Id: p.activeModelID}}, nil
}

func (p *providerBackedRoleContractProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}

func (p *providerBackedRoleContractProvider) WriteModel(context.Context, *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
	return nil, errors.New("WriteModel not implemented")
}

func providerBackedRoleRelationshipMatches(rel *core.Relationship, req *core.ReadRelationshipsRequest) bool {
	if rel == nil {
		return false
	}
	if subject := req.GetSubject(); subject != nil {
		if rel.GetSubject().GetType() != subject.GetType() || rel.GetSubject().GetId() != subject.GetId() {
			return false
		}
	}
	if resource := req.GetResource(); resource != nil {
		if rel.GetResource().GetType() != resource.GetType() || rel.GetResource().GetId() != resource.GetId() {
			return false
		}
	}
	if relation := req.GetRelation(); relation != "" && rel.GetRelation() != relation {
		return false
	}
	return true
}
