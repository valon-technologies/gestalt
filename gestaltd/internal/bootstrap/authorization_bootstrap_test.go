package bootstrap

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	gproto "google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

func TestBootstrapAuthorizationProviderStatePreservesRuntimeRelationships(t *testing.T) {
	t.Parallel()

	provider := &recordingAuthorizationProvider{
		listResourceTypePages: []*proto.ListActiveModelResourceTypesResponse{
			{
				ResourceTypes: []*proto.AuthorizationModelResourceType{
					testAuthorizationResourceType("team", proto.SourceLayer_SOURCE_LAYER_RUNTIME),
					testAuthorizationResourceType("github", proto.SourceLayer_SOURCE_LAYER_RUNTIME),
					testAuthorizationResourceType("ignored-static", proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG),
				},
				NextPageToken: "next-model",
			},
			{
				ResourceTypes: []*proto.AuthorizationModelResourceType{
					testAuthorizationResourceType("group", proto.SourceLayer_SOURCE_LAYER_RUNTIME),
				},
			},
		},
		listRelationshipsPages: []*proto.ListRelationshipsResponse{
			{
				Relationships: []*proto.Relationship{
					testAuthorizationRelationship("user:runtime", "viewer", "github", "repo-1", proto.SourceLayer_SOURCE_LAYER_RUNTIME),
					testAuthorizationRelationship("user:static", "viewer", "github", "repo-1", proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG),
				},
				NextPageToken: "next",
			},
			{
				Relationships: []*proto.Relationship{
					testAuthorizationRelationship("user:runtime-2", "editor", "team", "servicing", proto.SourceLayer_SOURCE_LAYER_RUNTIME),
				},
			},
		},
	}
	before := time.Now().Unix()
	cfg := &config.Config{
		Server: config.ServerConfig{Providers: config.ServerProvidersConfig{Authorization: "authz"}},
		Providers: config.ProvidersConfig{
			Authorization: map[string]*config.ProviderEntry{"authz": {Default: true}},
		},
		Authorization: config.AuthorizationConfig{
			Models: map[string]config.AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]config.AuthorizationResourceTypeDef{
						"github": {
							Relations: map[string]config.AuthorizationRelationDef{
								"viewer": {SubjectTypes: []string{"subject"}},
							},
							Actions: map[string]config.AuthorizationActionDef{
								"repos/list-for-authenticated-user": {Relations: []string{"viewer"}},
							},
						},
					},
				},
			},
			Relationships: []config.AuthorizationRelationshipDef{{
				Subject:  config.AuthorizationSubjectDef{Type: "subject", ID: "user:alice"},
				Relation: "viewer",
				Resource: config.AuthorizationResourceDef{Type: "github", ID: "repo-1"},
			}},
		},
	}

	err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider})
	if err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}

	if provider.setAuthorizationState == nil {
		t.Fatal("SetAuthorizationState was not called")
	}
	model := provider.setAuthorizationState.GetModel()
	wantID, err := authorizationModelContentHash(model)
	if err != nil {
		t.Fatalf("authorizationModelContentHash: %v", err)
	}
	if got := model.GetId(); got != wantID {
		t.Fatalf("model id = %q, want content hash %q", got, wantID)
	}
	version, err := strconv.ParseInt(model.GetVersion(), 10, 64)
	if err != nil {
		t.Fatalf("model version = %q, want epoch seconds: %v", model.GetVersion(), err)
	}
	if after := time.Now().Unix(); version < before || version > after {
		t.Fatalf("model version = %d, want between %d and %d", version, before, after)
	}
	github := model.GetResourceTypes()[0]
	if got := github.GetName(); got != "github" {
		t.Fatalf("resource type name = %q, want github", got)
	}
	if got := github.GetActions()[0].GetName(); got != "repos/list-for-authenticated-user" {
		t.Fatalf("action name = %q, want canonical operation id", got)
	}
	resourceTypeNames := []string{}
	for _, resourceType := range model.GetResourceTypes() {
		resourceTypeNames = append(resourceTypeNames, resourceType.GetName())
	}
	if want := []string{"github", "team", "group"}; !reflect.DeepEqual(resourceTypeNames, want) {
		t.Fatalf("model resource types = %#v, want %#v", resourceTypeNames, want)
	}
	if got := provider.resourceTypeRequests; len(got) != 2 {
		t.Fatalf("ListActiveModelResourceTypes requests = %d, want 2", len(got))
	} else if got[0].GetFilter().GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || got[0].GetPageToken() != "" || got[1].GetPageToken() != "next-model" {
		t.Fatalf("ListActiveModelResourceTypes requests = %#v", got)
	}
	if got := provider.listRequests; len(got) != 2 {
		t.Fatalf("ListRelationships requests = %d, want 2", len(got))
	} else if got[0].GetFilter().GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || got[0].GetPageToken() != "" || got[1].GetPageToken() != "next" {
		t.Fatalf("ListRelationships requests = %#v", got)
	}
	relationships := provider.setAuthorizationState.GetRelationships()
	if got, want := len(relationships), 3; got != want {
		t.Fatalf("SetAuthorizationState relationships count = %d, want %d", got, want)
	}
	if got := relationships[0].GetSourceLayer(); got != proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG {
		t.Fatalf("static relationship source layer = %v, want static config", got)
	}
	runtimeSubjects := []string{
		relationships[1].GetTuple().GetTarget().GetSubject().GetId(),
		relationships[2].GetTuple().GetTarget().GetSubject().GetId(),
	}
	if want := []string{"user:runtime", "user:runtime-2"}; !reflect.DeepEqual(runtimeSubjects, want) {
		t.Fatalf("runtime subjects = %#v, want %#v", runtimeSubjects, want)
	}
}

func TestStaticAuthorizationModelCarriesResourceTypeDefaultRole(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Authorization: config.AuthorizationConfig{
			Models: map[string]config.AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]config.AuthorizationResourceTypeDef{
						"github": {
							DefaultRole: "viewer",
							Relations:   map[string]config.AuthorizationRelationDef{"viewer": {SubjectTypes: []string{"subject"}}},
						},
						"docs": {
							DefaultRole: "reader",
							Relations:   map[string]config.AuthorizationRelationDef{"reader": {SubjectTypes: []string{"subject"}}},
						},
						"team": {
							Relations: map[string]config.AuthorizationRelationDef{"member": {SubjectTypes: []string{"subject"}}},
						},
					},
				},
			},
		},
	}

	model, err := staticAuthorizationModel(cfg)
	if err != nil {
		t.Fatalf("staticAuthorizationModel: %v", err)
	}
	roles := map[string]string{}
	for _, resourceType := range model.GetResourceTypes() {
		roles[resourceType.GetName()] = resourceType.GetDefaultRole()
	}
	if got := roles["github"]; got != "viewer" {
		t.Fatalf("github default role = %q, want viewer", got)
	}
	if got := roles["docs"]; got != "reader" {
		t.Fatalf("docs default role = %q, want reader", got)
	}
	if got := roles["team"]; got != "" {
		t.Fatalf("team default role = %q, want empty", got)
	}
}

func TestBootstrapAuthorizationProviderStateSkipsRemoteProvider(t *testing.T) {
	t.Parallel()

	provider := &recordingAuthorizationProvider{}
	cfg := &config.Config{
		Server: config.ServerConfig{
			Remote: "https://parent.gestalt.example",
			Providers: config.ServerProvidersConfig{
				Authorization: "authz",
			},
		},
		Providers: config.ProvidersConfig{
			Authorization: map[string]*config.ProviderEntry{"authz": {Default: true}},
		},
		Authorization: config.AuthorizationConfig{
			Models: map[string]config.AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]config.AuthorizationResourceTypeDef{
						"github": {
							Relations: map[string]config.AuthorizationRelationDef{
								"viewer": {SubjectTypes: []string{"subject"}},
							},
						},
					},
				},
			},
		},
	}

	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState != nil {
		t.Fatal("SetAuthorizationState should not be called for remote authorization providers")
	}
}

func TestBuildAuthorizationProviderPassesIndexedDBHostService(t *testing.T) {
	t.Parallel()

	var runtimeConfig yaml.Node
	if err := yaml.Unmarshal([]byte(`
command: /bin/true
config:
  indexeddb: main-db
`), &runtimeConfig); err != nil {
		t.Fatalf("decode runtime config: %v", err)
	}
	var capturedName string
	var capturedHostServices []runtimehost.HostService
	factories := &FactoryRegistry{
		Authorization: func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService, _ Deps) (providerdrivers.AuthorizationBuildResult, error) {
			capturedName = name
			capturedHostServices = append([]runtimehost.HostService(nil), hostServices...)
			provider := &recordingAuthorizationProvider{}
			return providerdrivers.AuthorizationBuildResult{Raw: provider}, nil
		},
	}
	cfg := &config.Config{
		Server: config.ServerConfig{Providers: config.ServerProvidersConfig{Authorization: "indexeddb"}},
		Providers: config.ProvidersConfig{
			Authorization: map[string]*config.ProviderEntry{
				"indexeddb": {Config: *runtimeConfig.Content[0]},
			},
		},
	}
	deps := Deps{
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		SelectedIndexedDBName: "main-db",
		IndexedDBs: map[string]indexeddb.IndexedDB{
			"main-db": &coretesting.StubIndexedDB{},
		},
	}

	providers, err := buildAuthorizationProviders(context.Background(), cfg, factories, deps)
	if err != nil {
		t.Fatalf("buildAuthorizationProviders: %v", err)
	}
	if providers.Raw["indexeddb"] == nil {
		t.Fatal("raw authorization provider was not built")
	}
	if capturedName != "indexeddb" {
		t.Fatalf("authorization provider name = %q, want indexeddb", capturedName)
	}
	for _, hostService := range capturedHostServices {
		if hostService.Name == "indexeddb" {
			return
		}
	}
	t.Fatalf("authorization host services = %#v, want indexeddb host service", capturedHostServices)
}

func TestBootstrapAuthorizationProviderStateAuthorizesProviderGatewayRequests(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{Providers: config.ServerProvidersConfig{Authorization: "authz"}},
		Providers: config.ProvidersConfig{
			Authorization: map[string]*config.ProviderEntry{"authz": {Default: true}},
		},
		Authorization: config.AuthorizationConfig{
			Models: map[string]config.AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]config.AuthorizationResourceTypeDef{
						"provider": {
							Relations: map[string]config.AuthorizationRelationDef{
								"viewer": {SubjectTypes: []string{"subject"}},
							},
							Actions: map[string]config.AuthorizationActionDef{
								"sync": {Relations: []string{"viewer"}},
							},
						},
					},
				},
			},
			Relationships: []config.AuthorizationRelationshipDef{{
				Subject:  config.AuthorizationSubjectDef{Type: "subject", ID: "user:alice"},
				Relation: "viewer",
				Resource: config.AuthorizationResourceDef{Type: "provider", ID: "github"},
			}},
		},
	}
	provider := &statefulAuthorizationProvider{}
	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}

	transport := providergateway.NewProviderGatewayTransport()
	transport.SetAuthorizationProvider(provider)

	allowedCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:alice"})
	if _, err := transport.Invoke(allowedCtx, providergateway.ProviderGatewayRequest{
		ProviderID:   "github",
		ProviderKind: providergateway.ProviderKindAuthorization,
		Operation:    "sync",
	}, func(_ context.Context, _ providergateway.ProviderGatewayRequest) (providergateway.ProviderGatewayResponse, error) {
		return providergateway.ProviderGatewayResponse{}, nil
	}); err != nil {
		t.Fatalf("Invoke allowed: %v", err)
	}

	deniedCtx := principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:bob"})
	if _, err := transport.Invoke(deniedCtx, providergateway.ProviderGatewayRequest{
		ProviderID:   "github",
		ProviderKind: providergateway.ProviderKindAuthorization,
		Operation:    "sync",
	}, func(_ context.Context, _ providergateway.ProviderGatewayRequest) (providergateway.ProviderGatewayResponse, error) {
		return providergateway.ProviderGatewayResponse{}, nil
	}); err == nil {
		t.Fatal("Invoke denied: expected unauthorized error")
	}
}

type recordingAuthorizationProvider struct {
	listRelationshipsPages []*proto.ListRelationshipsResponse
	listResourceTypePages  []*proto.ListActiveModelResourceTypesResponse
	listRequests           []*proto.ListRelationshipsRequest
	resourceTypeRequests   []*proto.ListActiveModelResourceTypesRequest
	setAuthorizationState  *proto.SetAuthorizationStateRequest
}

func (p *recordingAuthorizationProvider) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{}, nil
}

func (p *recordingAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *recordingAuthorizationProvider) ListRelationships(_ context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	p.listRequests = append(p.listRequests, gproto.Clone(req).(*proto.ListRelationshipsRequest))
	if len(p.listRelationshipsPages) == 0 {
		return &proto.ListRelationshipsResponse{}, nil
	}
	resp := p.listRelationshipsPages[0]
	p.listRelationshipsPages = p.listRelationshipsPages[1:]
	return gproto.Clone(resp).(*proto.ListRelationshipsResponse), nil
}

func (p *recordingAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *recordingAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *recordingAuthorizationProvider) SetAuthorizationState(_ context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	p.setAuthorizationState = gproto.Clone(req).(*proto.SetAuthorizationStateRequest)
	return &proto.SetAuthorizationStateResponse{ActiveModel: &proto.AuthorizationModelRef{
		Id:      req.GetModel().GetId(),
		Version: req.GetModel().GetVersion(),
	}}, nil
}

func (p *recordingAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *recordingAuthorizationProvider) SetActiveModel(_ context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{Model: &proto.AuthorizationModelRef{Id: req.GetModel().GetId(), Version: req.GetModel().GetVersion()}}, nil
}

func (p *recordingAuthorizationProvider) ListActiveModelResourceTypes(_ context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	p.resourceTypeRequests = append(p.resourceTypeRequests, gproto.Clone(req).(*proto.ListActiveModelResourceTypesRequest))
	if len(p.listResourceTypePages) == 0 {
		return &proto.ListActiveModelResourceTypesResponse{}, nil
	}
	resp := p.listResourceTypePages[0]
	p.listResourceTypePages = p.listResourceTypePages[1:]
	return gproto.Clone(resp).(*proto.ListActiveModelResourceTypesResponse), nil
}

func (p *recordingAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *recordingAuthorizationProvider) Close() error { return nil }

func testAuthorizationRelationship(subjectID, relation, resourceType, resourceID string, layer proto.SourceLayer) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: subjectID}},
			},
			Relation: relation,
			Resource: &proto.Resource{Type: resourceType, Id: resourceID},
		},
		SourceLayer: layer,
	}
}

func testAuthorizationResourceType(name string, layer proto.SourceLayer) *proto.AuthorizationModelResourceType {
	return &proto.AuthorizationModelResourceType{
		Name:        name,
		SourceLayer: layer,
		Relations: []*proto.ModelRelation{{
			Name: "member",
			AllowedTargets: []*proto.ModelAllowedTarget{{
				Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"},
			}},
		}},
	}
}

type statefulAuthorizationProvider struct {
	model         *proto.AuthorizationModel
	relationships []*proto.Relationship
}

func (p *statefulAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if p == nil || req == nil || req.GetSubject() == nil || req.GetResource() == nil || req.GetAction() == nil || p.model == nil {
		return &proto.CheckAccessResponse{Allowed: false}, nil
	}
	var action *proto.ModelAction
	for _, resourceType := range p.model.GetResourceTypes() {
		if resourceType.GetName() != req.GetResource().GetType() {
			continue
		}
		for _, candidate := range resourceType.GetActions() {
			if candidate.GetName() == req.GetAction().GetName() {
				action = candidate
				break
			}
		}
		break
	}
	if action == nil {
		return &proto.CheckAccessResponse{Allowed: false}, nil
	}
	for _, relationName := range action.GetRelations() {
		for _, relationship := range p.relationships {
			tuple := relationship.GetTuple()
			target := tuple.GetTarget().GetSubject()
			resource := tuple.GetResource()
			if target == nil || resource == nil {
				continue
			}
			if target.GetType() == req.GetSubject().GetType() &&
				target.GetId() == req.GetSubject().GetId() &&
				resource.GetType() == req.GetResource().GetType() &&
				resource.GetId() == req.GetResource().GetId() &&
				tuple.GetRelation() == relationName {
				return &proto.CheckAccessResponse{Allowed: true, ModelId: p.model.GetId()}, nil
			}
		}
	}
	return &proto.CheckAccessResponse{Allowed: false, ModelId: p.model.GetId()}, nil
}

func (p *statefulAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (p *statefulAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (p *statefulAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (p *statefulAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (p *statefulAuthorizationProvider) SetAuthorizationState(_ context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	p.model = gproto.Clone(req.GetModel()).(*proto.AuthorizationModel)
	p.relationships = make([]*proto.Relationship, 0, len(req.GetRelationships()))
	for _, relationship := range req.GetRelationships() {
		p.relationships = append(p.relationships, gproto.Clone(relationship).(*proto.Relationship))
	}
	return &proto.SetAuthorizationStateResponse{ActiveModel: &proto.AuthorizationModelRef{
		Id:      req.GetModel().GetId(),
		Version: req.GetModel().GetVersion(),
	}}, nil
}

func (p *statefulAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (p *statefulAuthorizationProvider) SetActiveModel(_ context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{Model: &proto.AuthorizationModelRef{Id: req.GetModel().GetId(), Version: req.GetModel().GetVersion()}}, nil
}

func (p *statefulAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (p *statefulAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *statefulAuthorizationProvider) Close() error { return nil }

func TestProviderAuthorizationKindsSkipsDedicatedAppResourceTypes(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"workspace_review": {},
			"legacy_app":       {},
		},
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{"nightly": {}},
			Agent:    map[string]*config.ProviderEntry{"assistant": {}},
		},
		Authorization: config.AuthorizationConfig{
			Models: map[string]config.AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]config.AuthorizationResourceTypeDef{
						"legacy_app": {},
					},
				},
			},
		},
	}

	got := ProviderAuthorizationKinds(cfg)
	if _, ok := got["workspace_review"]; !ok || got["workspace_review"] != invocation.ProviderKindApp {
		t.Fatalf("workspace_review kind = %v, want app", got["workspace_review"])
	}
	if _, ok := got["legacy_app"]; ok {
		t.Fatalf("legacy_app should be excluded from provider kinds, got %v", got["legacy_app"])
	}
	if got["nightly"] != invocation.ProviderKindWorkflow {
		t.Fatalf("nightly kind = %v, want workflow", got["nightly"])
	}
	if got["assistant"] != invocation.ProviderKindAgent {
		t.Fatalf("assistant kind = %v, want agent", got["assistant"])
	}
}
