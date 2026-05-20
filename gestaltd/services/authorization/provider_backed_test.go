package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/protobuf/types/known/structpb"
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

func TestProviderBackedReloadComposesConfigAndDynamicFragmentSources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	legacyDynamic := &core.Relationship{
		Target: &core.RelationshipTargetRef{Kind: &proto.RelationshipTarget_Subject{Subject: &core.SubjectRef{
			Type: subjectTypeSubject,
			Id:   "user:alice",
		}}},
		Relation:   "editor",
		Resource:   &core.ResourceRef{Type: resourceTypePluginDynamic, Id: "slack"},
		Properties: mustStruct(t, map[string]any{"source": "provider"}),
	}
	provider := newProviderBackedComposerTestProvider("model-0", legacyDynamic)
	if _, err := services.AuthzFragments.PutFragment(ctx, &coredata.AuthorizationDynamicFragment{
		Owner: coredata.AuthorizationGlobalFragmentOwner(),
		ResourceTypes: map[string]json.RawMessage{
			"project": json.RawMessage(`{"relations":{"viewer":{"subjectTypes":["subject"]}},"actions":{"view":{"relations":["viewer"]}}}`),
		},
	}, coredata.AuthorizationDynamicFragmentUpdate{Audit: coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "test_dynamic_model"}}); err != nil {
		t.Fatalf("PutFragment: %v", err)
	}
	base, err := New(StaticConfig{
		ModelFragments: []*core.AuthorizationModelResourceType{{
			Name: "workspace",
			Relations: []*core.AuthorizationModelRelation{{
				Name:         "member",
				SubjectTypes: []string{subjectTypeSubject},
			}},
		}},
		Relationships: []*core.Relationship{providerBackedRoleTestRelationship("user:bob", "workspace", "ws-1", "member")},
	})
	if err != nil {
		t.Fatalf("New authorizer: %v", err)
	}
	authorizer, err := NewProviderBacked(base, provider, WithDynamicFragmentSource(services.AuthzFragments))
	if err != nil {
		t.Fatalf("NewProviderBacked: %v", err)
	}

	if err := authorizer.ReloadAuthorizationState(ctx); err != nil {
		t.Fatalf("ReloadAuthorizationState: %v", err)
	}
	if authorizationModelResourceType(provider.lastModel, "workspace") == nil {
		t.Fatalf("composed model resource types = %#v, want workspace fragment", provider.lastModel.GetResourceTypes())
	}
	if authorizationModelResourceType(provider.lastModel, "project") == nil {
		t.Fatalf("composed model resource types = %#v, want project fragment", provider.lastModel.GetResourceTypes())
	}
	if !provider.hasRelationship(legacyDynamic) {
		t.Fatal("provider is missing backfilled plugin dynamic relationship")
	}
	if !provider.hasRelationship(providerBackedRoleTestRelationship("user:bob", "workspace", "ws-1", "member")) {
		t.Fatal("provider is missing static config relationship")
	}
	fragment, err := services.AuthzFragments.GetFragmentByOwner(ctx, coredata.AuthorizationPluginFragmentOwner("slack"))
	if err != nil {
		t.Fatalf("GetFragmentByOwner: %v", err)
	}
	if len(fragment.Relationships) != 1 || fragment.Relationships[0].Subject.ID != "user:alice" {
		t.Fatalf("backfilled fragment relationships = %#v", fragment.Relationships)
	}
	if fragment.Relationships[0].Target.Subject == nil || fragment.Relationships[0].Target.Subject.ID != "user:alice" {
		t.Fatalf("backfilled fragment target = %#v, want subject target", fragment.Relationships[0].Target)
	}
	if fragment.Relationships[0].Properties["source"] != "provider" {
		t.Fatalf("backfilled fragment properties = %#v, want provider source", fragment.Relationships[0].Properties)
	}

	deleted, _, err := services.AuthzFragments.DeleteRelationship(ctx, coredata.AuthorizationPluginFragmentOwner("slack"), fragment.Relationships[0], coredata.AuthorizationDynamicFragmentAuditMetadata{Reason: "test_delete"})
	if err != nil {
		t.Fatalf("DeleteRelationship: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteRelationship deleted = false, want true")
	}
	if err := authorizer.ReloadAuthorizationState(ctx); err != nil {
		t.Fatalf("ReloadAuthorizationState after delete: %v", err)
	}
	if provider.hasRelationship(legacyDynamic) {
		t.Fatal("provider still has plugin dynamic relationship after source fragment deletion")
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

func mustStruct(t *testing.T, fields map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	return out
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

func (p *providerBackedComposerTestProvider) Evaluate(context.Context, *core.AccessEvaluationRequest) (*core.AccessDecision, error) {
	return &core.AccessDecision{}, nil
}

func (p *providerBackedComposerTestProvider) EvaluateMany(context.Context, *core.AccessEvaluationsRequest) (*core.AccessEvaluationsResponse, error) {
	return &core.AccessEvaluationsResponse{}, nil
}

func (p *providerBackedComposerTestProvider) SearchResources(context.Context, *core.ResourceSearchRequest) (*core.ResourceSearchResponse, error) {
	return nil, errors.New("SearchResources not implemented")
}

func (p *providerBackedComposerTestProvider) SearchSubjects(context.Context, *core.SubjectSearchRequest) (*core.SubjectSearchResponse, error) {
	return nil, errors.New("SearchSubjects not implemented")
}

func (p *providerBackedComposerTestProvider) SearchActions(context.Context, *core.ActionSearchRequest) (*core.ActionSearchResponse, error) {
	return nil, errors.New("SearchActions not implemented")
}

func (p *providerBackedComposerTestProvider) GetMetadata(context.Context) (*core.AuthorizationMetadata, error) {
	return &core.AuthorizationMetadata{}, nil
}

func (p *providerBackedComposerTestProvider) ReadRelationships(_ context.Context, req *core.ReadRelationshipsRequest) (*core.ReadRelationshipsResponse, error) {
	modelID := req.GetModelId()
	if modelID == "" {
		modelID = p.activeModelID
	}
	out := []*core.Relationship{}
	for _, rel := range p.relationshipsByModelID[modelID] {
		if !providerBackedRoleRelationshipMatches(rel, req) {
			continue
		}
		out = append(out, rel)
	}
	return &core.ReadRelationshipsResponse{Relationships: out, ModelId: modelID}, nil
}

func (p *providerBackedComposerTestProvider) WriteRelationships(_ context.Context, req *core.WriteRelationshipsRequest) error {
	modelID := req.GetModelId()
	if modelID == "" {
		modelID = p.activeModelID
	}
	if p.relationshipsByModelID[modelID] == nil {
		p.relationshipsByModelID[modelID] = map[string]*core.Relationship{}
	}
	for _, key := range req.GetDeletes() {
		delete(p.relationshipsByModelID[modelID], relationshipKeyMapKey(key))
	}
	for _, rel := range req.GetWrites() {
		p.relationshipsByModelID[modelID][relationshipMapKey(rel)] = rel
	}
	return nil
}

func (p *providerBackedComposerTestProvider) GetActiveModel(context.Context) (*core.GetActiveModelResponse, error) {
	return &core.GetActiveModelResponse{Model: &core.AuthorizationModelRef{Id: p.activeModelID}}, nil
}

func (p *providerBackedComposerTestProvider) ListModels(context.Context, *core.ListModelsRequest) (*core.ListModelsResponse, error) {
	return &core.ListModelsResponse{}, nil
}

func (p *providerBackedComposerTestProvider) WriteModel(_ context.Context, req *core.WriteModelRequest) (*core.AuthorizationModelRef, error) {
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
	return &core.AuthorizationModelRef{Id: modelID}, nil
}

func (p *providerBackedComposerTestProvider) hasRelationship(rel *core.Relationship) bool {
	return p.relationshipsByModelID[p.activeModelID][relationshipMapKey(rel)] != nil
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
