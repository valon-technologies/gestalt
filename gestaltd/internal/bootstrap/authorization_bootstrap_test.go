package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
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
		Server: config.ServerConfig{
			Providers:               config.ServerProvidersConfig{Authorization: "authz"},
			AuthorizationStateApply: boolPtr(true),
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

	err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil)
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
			Remotes: map[string]*config.RemoteConfig{
				config.DefaultRemoteName: {
					URL:     "https://parent.gestalt.example",
					Default: true,
				},
			},
			Providers: config.ServerProvidersConfig{
				Authorization: "authz",
			},
		},
		Providers: config.ProvidersConfig{
			Authorization: map[string]*config.ProviderEntry{
				"authz": {Default: true, Remote: config.DefaultRemoteName},
			},
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

	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil); err != nil {
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
	if providers["indexeddb"] == nil {
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
		Server: config.ServerConfig{
			Providers:               config.ServerProvidersConfig{Authorization: "authz"},
			AuthorizationStateApply: boolPtr(true),
		},
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
	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}

	allowed, err := invocation.CheckSubjectAccess(context.Background(), provider, invocation.SubjectAccessRequest("user:alice", "sync", &proto.Resource{Type: "provider", Id: "github"}))
	if err != nil {
		t.Fatalf("CheckSubjectAccess allowed: %v", err)
	}
	if !allowed {
		t.Fatal("CheckSubjectAccess allowed: expected allowed for user:alice")
	}

	allowed, err = invocation.CheckSubjectAccess(context.Background(), provider, invocation.SubjectAccessRequest("user:bob", "sync", &proto.Resource{Type: "provider", Id: "github"}))
	if err != nil {
		t.Fatalf("CheckSubjectAccess denied: %v", err)
	}
	if allowed {
		t.Fatal("CheckSubjectAccess denied: expected denied for user:bob")
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

func TestProviderAuthorizationPolicies(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"shared": {AuthorizationPolicy: " workspace "},
			"own":    {},
		},
	}

	got := ProviderAuthorizationPolicies(cfg)
	if got["shared"] != "workspace" {
		t.Fatalf("shared policy = %q, want workspace", got["shared"])
	}
	if _, ok := got["own"]; ok {
		t.Fatal("own app should use its app name instead of an explicit policy")
	}
}

func TestBootstrapAuthorizationProviderReceivesSharedGatewayTransport(t *testing.T) {
	t.Parallel()

	var capturedTransport *providergateway.ProviderGatewayTransport
	factories := &FactoryRegistry{
		Auth: func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (core.IdentityProvider, error) {
			return &coretesting.StubAuthProvider{}, nil
		},
		Authorization: func(_ context.Context, _ string, _ yaml.Node, _ []runtimehost.HostService, deps Deps) (providerdrivers.AuthorizationBuildResult, error) {
			capturedTransport = deps.GatewayTransport
			return providerdrivers.AuthorizationBuildResult{Raw: &recordingAuthorizationProvider{}}, nil
		},
	}
	factories.Secrets = map[string]SecretManagerFactory{
		"stub": func(yaml.Node) (core.SecretManager, error) {
			return &coretesting.StubSecretManager{Secrets: map[string]string{}}, nil
		},
	}
	factories.Telemetry = map[string]TelemetryFactory{
		"noop": func(yaml.Node) (core.TelemetryProvider, error) {
			return telemetrynoopProvider{}, nil
		},
	}
	factories.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		return &coretesting.StubIndexedDB{}, nil
	}
	factories.ExternalCredentials = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (core.ExternalCredentialProvider, error) {
		return coretesting.NewStubExternalCredentialProvider(), nil
	}

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Identity: map[string]*config.ProviderEntry{
				"default": {
					Source: config.NewMetadataSource("https://example.invalid/auth/oidc/v0.0.1/provider-release.yaml"),
					Config: yaml.Node{Kind: yaml.MappingNode},
				},
			},
			Secrets: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "stub"}},
			},
			Telemetry: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "noop"}},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"main": {Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml")},
			},
			Authorization: map[string]*config.ProviderEntry{
				"authz": {Config: yaml.Node{Kind: yaml.MappingNode}},
			},
		},
		Server: config.ServerConfig{
			Providers:     config.ServerProvidersConfig{IndexedDB: "main"},
			EncryptionKey: "test-key",
		},
	}

	result, err := Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = result.Close(context.Background()) })

	if capturedTransport == nil {
		t.Fatal("factory did not receive a GatewayTransport in Deps")
	}
	if result.PublicGatewayTransport != capturedTransport {
		t.Fatal("factory GatewayTransport is not the same instance as the public transport")
	}
}

type telemetrynoopProvider struct{}

func (telemetrynoopProvider) Logger() *slog.Logger                 { return slog.Default() }
func (telemetrynoopProvider) PrometheusHandler() http.Handler      { return nil }
func (telemetrynoopProvider) MeterProvider() metric.MeterProvider  { return nil }
func (telemetrynoopProvider) TracerProvider() trace.TracerProvider { return nil }
func (telemetrynoopProvider) Shutdown(context.Context) error       { return nil }

func boolPtr(v bool) *bool { return &v }

func authorizationBootstrapTestConfig(applyEnabled *bool) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Providers:               config.ServerProvidersConfig{Authorization: "authz"},
			AuthorizationStateApply: applyEnabled,
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
}

type stubAuthorizationUserResolver struct {
	users map[string]*core.User
	err   error
	calls []string
}

func (s *stubAuthorizationUserResolver) FindUserByEmail(_ context.Context, email string) (*core.User, error) {
	s.calls = append(s.calls, email)
	if s.err != nil {
		return nil, s.err
	}
	user, ok := s.users[email]
	if !ok {
		return nil, core.ErrNotFound
	}
	return user, nil
}

func TestStaticAuthorizationRelationshipsResolveSubjectEmails(t *testing.T) {
	t.Parallel()

	cfg := config.AuthorizationConfig{
		Relationships: []config.AuthorizationRelationshipDef{
			{
				Subject:  config.AuthorizationSubjectDef{Type: "subject", Email: "alice@example.com"},
				Relation: "viewer",
				Resource: config.AuthorizationResourceDef{Type: "app", ID: "one"},
			},
			{
				Target: config.AuthorizationRelationshipTargetDef{
					Subject: &config.AuthorizationSubjectDef{Type: "subject", Email: "bob@example.com"},
				},
				Relation: "admin",
				Resource: config.AuthorizationResourceDef{Type: "app", ID: "two"},
			},
			{
				Subject:  config.AuthorizationSubjectDef{Type: "subject", Email: "missing@example.com"},
				Relation: "viewer",
				Resource: config.AuthorizationResourceDef{Type: "app", ID: "skipped"},
			},
			{
				Subject:  config.AuthorizationSubjectDef{Type: "subject", ID: "service_account:unchanged"},
				Relation: "viewer",
				Resource: config.AuthorizationResourceDef{Type: "app", ID: "three"},
			},
			{
				Subject: config.AuthorizationSubjectDef{Type: "subject", Email: "ignored@example.com"},
				Target: config.AuthorizationRelationshipTargetDef{
					Resource: &config.AuthorizationResourceDef{Type: "app", ID: "parent"},
				},
				Relation: "viewer",
				Resource: config.AuthorizationResourceDef{Type: "app", ID: "child"},
			},
		},
	}
	users := &stubAuthorizationUserResolver{users: map[string]*core.User{
		"alice@example.com": {ID: "11111111-1111-4111-8111-111111111111"},
		"bob@example.com":   {ID: "22222222-2222-4222-8222-222222222222"},
	}}

	relationships, err := staticAuthorizationRelationships(context.Background(), cfg, users)
	if err != nil {
		t.Fatalf("staticAuthorizationRelationships: %v", err)
	}
	if got, want := len(relationships), 4; got != want {
		t.Fatalf("relationships count = %d, want %d", got, want)
	}
	gotSubjects := []string{
		relationships[0].GetTuple().GetTarget().GetSubject().GetId(),
		relationships[1].GetTuple().GetTarget().GetSubject().GetId(),
		relationships[2].GetTuple().GetTarget().GetSubject().GetId(),
	}
	wantSubjects := []string{
		"user:11111111-1111-4111-8111-111111111111",
		"user:22222222-2222-4222-8222-222222222222",
		"service_account:unchanged",
	}
	if !reflect.DeepEqual(gotSubjects, wantSubjects) {
		t.Fatalf("relationship subjects = %#v, want %#v", gotSubjects, wantSubjects)
	}
	if got, want := relationships[3].GetTuple().GetTarget().GetResource().GetId(), "parent"; got != want {
		t.Fatalf("resource target id = %q, want %q", got, want)
	}
	if wantCalls := []string{"alice@example.com", "bob@example.com", "missing@example.com"}; !reflect.DeepEqual(users.calls, wantCalls) {
		t.Fatalf("user lookup calls = %#v, want %#v", users.calls, wantCalls)
	}
}

func TestStaticAuthorizationRelationshipsAbortOnUserLookupFailure(t *testing.T) {
	t.Parallel()

	cfg := config.AuthorizationConfig{
		Relationships: []config.AuthorizationRelationshipDef{{
			Subject:  config.AuthorizationSubjectDef{Type: "subject", Email: "alice@example.com"},
			Relation: "viewer",
			Resource: config.AuthorizationResourceDef{Type: "app", ID: "one"},
		}},
	}
	users := &stubAuthorizationUserResolver{err: errors.New("database unavailable")}

	_, err := staticAuthorizationRelationships(context.Background(), cfg, users)
	if err == nil || !strings.Contains(err.Error(), `resolve subject email "alice@example.com": database unavailable`) {
		t.Fatalf("staticAuthorizationRelationships error = %v, want lookup failure", err)
	}
}

func TestBootstrapAuthorizationProviderStateResolvesEmailsInPlanOnlyMode(t *testing.T) {
	t.Parallel()

	provider := &recordingAuthorizationProvider{}
	cfg := authorizationBootstrapTestConfig(boolPtr(false))
	cfg.Authorization.Relationships[0].Subject = config.AuthorizationSubjectDef{
		Type:  "subject",
		Email: "alice@example.com",
	}
	users := &stubAuthorizationUserResolver{users: map[string]*core.User{
		"alice@example.com": {ID: "11111111-1111-4111-8111-111111111111"},
	}}

	if err := bootstrapAuthorizationProviderState(
		context.Background(),
		cfg,
		map[string]core.AuthorizationProvider{"authz": provider},
		users,
	); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState != nil {
		t.Fatal("SetAuthorizationState should not be called in plan-only mode")
	}
	if want := []string{"alice@example.com"}; !reflect.DeepEqual(users.calls, want) {
		t.Fatalf("user lookup calls = %#v, want %#v", users.calls, want)
	}
}

func TestBootstrapAuthorizationProviderStateAppliesCanonicalEmailSubject(t *testing.T) {
	t.Parallel()

	provider := &recordingAuthorizationProvider{}
	cfg := authorizationBootstrapTestConfig(boolPtr(true))
	cfg.Authorization.Relationships[0].Subject = config.AuthorizationSubjectDef{
		Type:  "subject",
		Email: "alice@example.com",
	}
	users := &stubAuthorizationUserResolver{users: map[string]*core.User{
		"alice@example.com": {ID: "11111111-1111-4111-8111-111111111111"},
	}}

	if err := bootstrapAuthorizationProviderState(
		context.Background(),
		cfg,
		map[string]core.AuthorizationProvider{"authz": provider},
		users,
	); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	relationships := provider.setAuthorizationState.GetRelationships()
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("applied relationships count = %d, want %d", got, want)
	}
	if got, want := relationships[0].GetTuple().GetTarget().GetSubject().GetId(), "user:11111111-1111-4111-8111-111111111111"; got != want {
		t.Fatalf("applied subject id = %q, want %q", got, want)
	}
}

func TestBootstrapAuthorizationProviderStateDefaultsToPlanOnly(t *testing.T) {
	t.Parallel()

	provider := &recordingAuthorizationProvider{}
	cfg := authorizationBootstrapTestConfig(nil)

	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState != nil {
		t.Fatal("SetAuthorizationState should not be called when authorization state apply is not enabled")
	}
}

func TestBootstrapAuthorizationProviderStateExplicitFalseSkipsApply(t *testing.T) {
	t.Parallel()

	provider := &recordingAuthorizationProvider{}
	cfg := authorizationBootstrapTestConfig(boolPtr(false))

	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState != nil {
		t.Fatal("SetAuthorizationState should not be called when authorization state apply is explicitly disabled")
	}
}

func TestBootstrapAuthorizationProviderStateEnvEnablesApply(t *testing.T) {
	t.Setenv(authorizationStateApplyEnv, "true")

	provider := &recordingAuthorizationProvider{}
	cfg := authorizationBootstrapTestConfig(nil)

	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState == nil {
		t.Fatal("SetAuthorizationState should be called when GESTALTD_AUTHORIZATION_STATE_APPLY=true")
	}
}

func TestBootstrapAuthorizationProviderStateConfigOverridesEnv(t *testing.T) {
	t.Setenv(authorizationStateApplyEnv, "true")

	provider := &recordingAuthorizationProvider{}
	cfg := authorizationBootstrapTestConfig(boolPtr(false))

	if err := bootstrapAuthorizationProviderState(context.Background(), cfg, map[string]core.AuthorizationProvider{"authz": provider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState: %v", err)
	}
	if provider.setAuthorizationState != nil {
		t.Fatal("explicit config false should override a truthy env var")
	}
}

func TestBootstrapAuthorizationProviderStatePlanAndApplyProduceSameDigest(t *testing.T) {
	t.Parallel()

	planProvider := &recordingAuthorizationProvider{}
	planCfg := authorizationBootstrapTestConfig(boolPtr(false))
	if err := bootstrapAuthorizationProviderState(context.Background(), planCfg, map[string]core.AuthorizationProvider{"authz": planProvider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState (plan): %v", err)
	}
	planModel, err := staticAuthorizationModel(planCfg)
	if err != nil {
		t.Fatalf("staticAuthorizationModel: %v", err)
	}
	if err := stampAuthorizationModel(planModel, time.Now()); err != nil {
		t.Fatalf("stampAuthorizationModel: %v", err)
	}
	planDigest := planModel.GetId()

	applyProvider := &recordingAuthorizationProvider{}
	applyCfg := authorizationBootstrapTestConfig(boolPtr(true))
	if err := bootstrapAuthorizationProviderState(context.Background(), applyCfg, map[string]core.AuthorizationProvider{"authz": applyProvider}, nil); err != nil {
		t.Fatalf("bootstrapAuthorizationProviderState (apply): %v", err)
	}
	if applyProvider.setAuthorizationState == nil {
		t.Fatal("SetAuthorizationState should be called when apply is enabled")
	}
	applyDigest := applyProvider.setAuthorizationState.GetModel().GetId()

	if planDigest == "" || applyDigest == "" {
		t.Fatalf("digests should be non-empty: plan=%q apply=%q", planDigest, applyDigest)
	}
	if planDigest != applyDigest {
		t.Fatalf("plan digest %q should match apply digest %q for identical static models", planDigest, applyDigest)
	}
}

func TestAuthorizationModelContentHashDeterministicAndSensitiveToChanges(t *testing.T) {
	t.Parallel()

	cfg := authorizationBootstrapTestConfig(nil)
	modelA, err := staticAuthorizationModel(cfg)
	if err != nil {
		t.Fatalf("staticAuthorizationModel: %v", err)
	}
	modelB, err := staticAuthorizationModel(cfg)
	if err != nil {
		t.Fatalf("staticAuthorizationModel: %v", err)
	}
	digestA, err := authorizationModelContentHash(modelA)
	if err != nil {
		t.Fatalf("authorizationModelContentHash: %v", err)
	}
	digestB, err := authorizationModelContentHash(modelB)
	if err != nil {
		t.Fatalf("authorizationModelContentHash: %v", err)
	}
	if digestA != digestB {
		t.Fatalf("digest should be deterministic for identical models: %q != %q", digestA, digestB)
	}

	cfg.Authorization.Models["default"].ResourceTypes["docs"] = config.AuthorizationResourceTypeDef{
		Relations: map[string]config.AuthorizationRelationDef{"reader": {SubjectTypes: []string{"subject"}}},
	}
	modelC, err := staticAuthorizationModel(cfg)
	if err != nil {
		t.Fatalf("staticAuthorizationModel: %v", err)
	}
	digestC, err := authorizationModelContentHash(modelC)
	if err != nil {
		t.Fatalf("authorizationModelContentHash: %v", err)
	}
	if digestC == digestA {
		t.Fatal("digest should change when the static model gains a resource type")
	}
}
