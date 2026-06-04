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

func TestStaticAuthorizationModelCarriesResourceTypeDefaultAccessPolicy(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Authorization: config.AuthorizationConfig{
			Models: map[string]config.AuthorizationModelDef{
				"default": {
					ResourceTypes: map[string]config.AuthorizationResourceTypeDef{
						"github": {
							DefaultAccessPolicy: "allow",
							Relations:           map[string]config.AuthorizationRelationDef{"viewer": {SubjectTypes: []string{"subject"}}},
						},
						"slack": {
							DefaultAccessPolicy: "deny",
							Relations:           map[string]config.AuthorizationRelationDef{"viewer": {SubjectTypes: []string{"subject"}}},
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
	policies := map[string]proto.DefaultAccessPolicy{}
	for _, resourceType := range model.GetResourceTypes() {
		policies[resourceType.GetName()] = resourceType.GetDefaultAccessPolicy()
	}
	if got := policies["github"]; got != proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_ALLOW {
		t.Fatalf("github default policy = %v, want allow", got)
	}
	if got := policies["slack"]; got != proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_DENY {
		t.Fatalf("slack default policy = %v, want deny", got)
	}
	if got := policies["team"]; got != proto.DefaultAccessPolicy_DEFAULT_ACCESS_POLICY_DENY {
		t.Fatalf("team default policy = %v, want deny", got)
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
		Authorization: func(_ context.Context, name string, _ yaml.Node, hostServices []runtimehost.HostService) (core.AuthorizationProvider, error) {
			capturedName = name
			capturedHostServices = append([]runtimehost.HostService(nil), hostServices...)
			return &recordingAuthorizationProvider{}, nil
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
		t.Fatal("authorization provider was not built")
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
