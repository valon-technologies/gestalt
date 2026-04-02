package gestalt_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type stubProvider struct {
	name        string
	displayName string
	description string
	connMode    gestalt.ConnectionMode
	catalog     *gestalt.Catalog
}

func (p *stubProvider) Name() string                           { return p.name }
func (p *stubProvider) DisplayName() string                    { return p.displayName }
func (p *stubProvider) Description() string                    { return p.description }
func (p *stubProvider) ConnectionMode() gestalt.ConnectionMode { return p.connMode }
func (p *stubProvider) Catalog() *gestalt.Catalog              { return p.catalog }

func (p *stubProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*gestalt.OperationResult, error) {
	return &gestalt.OperationResult{
		Status: 200,
		Body:   `{"operation":"` + operation + `"}`,
	}, nil
}

type startableStubProvider struct {
	stubProvider
	startName   string
	startConfig map[string]any
}

func (p *startableStubProvider) Start(_ context.Context, name string, config map[string]any) error {
	p.startName = name
	p.startConfig = config
	return nil
}

type schemaStubProvider struct {
	stubProvider
	schemaDocument string
}

func (p *schemaStubProvider) ConfigSchema() string { return p.schemaDocument }

type manualAuthStubProvider struct {
	stubProvider
}

func (p *manualAuthStubProvider) SupportsManualAuth() bool { return true }

type sessionCatalogStubProvider struct {
	stubProvider
	sessionCatalog *gestalt.Catalog
}

func (p *sessionCatalogStubProvider) CatalogForRequest(_ context.Context, _ string) (*gestalt.Catalog, error) {
	return p.sessionCatalog, nil
}

func TestProviderServerGetMetadata(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:        "test-provider",
		displayName: "Test Provider",
		description: "A test provider for SDK validation",
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetName() != "test-provider" {
		t.Errorf("Name = %q, want %q", meta.GetName(), "test-provider")
	}
	if meta.GetDisplayName() != "Test Provider" {
		t.Errorf("DisplayName = %q, want %q", meta.GetDisplayName(), "Test Provider")
	}
	if meta.GetConnectionMode() != proto.ConnectionMode_CONNECTION_MODE_NONE {
		t.Errorf("ConnectionMode = %v, want CONNECTION_MODE_NONE", meta.GetConnectionMode())
	}
	if len(meta.GetAuthTypes()) != 0 {
		t.Errorf("AuthTypes = %v, want empty for plain provider", meta.GetAuthTypes())
	}
}

func TestProviderServerGetMetadata_ManualAuth(t *testing.T) {
	t.Parallel()

	prov := &manualAuthStubProvider{
		stubProvider: stubProvider{name: "manual-prov"},
	}

	client := newProviderPluginClient(t, prov)
	meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	authTypes := meta.GetAuthTypes()
	if len(authTypes) != 1 || authTypes[0] != "manual" {
		t.Fatalf("AuthTypes = %v, want [manual]", authTypes)
	}
}

func TestProviderServerGetMetadata_StaticCatalog(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: gestalt.ConnectionModeNone,
		catalog: &gestalt.Catalog{
			Name: "test-provider",
			Operations: []gestalt.CatalogOperation{
				{
					ID:          "list_items",
					Description: "List all items",
					Method:      http.MethodGet,
					Parameters: []gestalt.CatalogParameter{
						{Name: "limit", Type: "integer", Description: "Max results", Default: 10},
					},
				},
			},
		},
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetStaticCatalogJson() == "" {
		t.Fatal("expected static catalog json")
	}
	var cat map[string]any
	if err := json.Unmarshal([]byte(meta.GetStaticCatalogJson()), &cat); err != nil {
		t.Fatalf("unmarshal static catalog: %v", err)
	}
	ops, ok := cat["operations"].([]any)
	if !ok || len(ops) != 1 {
		t.Fatalf("unexpected operations payload: %+v", cat["operations"])
	}
	first, ok := ops[0].(map[string]any)
	if !ok || first["id"] != "list_items" {
		t.Fatalf("unexpected first operation: %+v", ops[0])
	}
	if first["transport"] != "plugin" {
		t.Fatalf("unexpected first operation transport: %+v", ops[0])
	}
}

func TestProviderServerGetSessionCatalog(t *testing.T) {
	t.Parallel()

	prov := &sessionCatalogStubProvider{
		stubProvider: stubProvider{
			name:     "test-provider",
			connMode: gestalt.ConnectionModeUser,
			catalog: &gestalt.Catalog{
				Name: "test-provider",
				Operations: []gestalt.CatalogOperation{
					{ID: "static_op", Method: http.MethodGet},
				},
			},
		},
		sessionCatalog: &gestalt.Catalog{
			Name: "test-provider",
			Operations: []gestalt.CatalogOperation{
				{ID: "session_op", Method: http.MethodPost},
			},
		},
	}

	client := newProviderPluginClient(t, prov)
	resp, err := client.GetSessionCatalog(context.Background(), &proto.GetSessionCatalogRequest{Token: "tok"})
	if err != nil {
		t.Fatalf("GetSessionCatalog: %v", err)
	}
	if resp.GetCatalogJson() == "" {
		t.Fatal("expected session catalog json")
	}
}

func TestProviderServerExecute(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: gestalt.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	params, _ := structpb.NewStruct(map[string]any{"key": "value"})
	resp, err := client.Execute(ctx, &proto.ExecuteRequest{
		Operation: "test_op",
		Params:    params,
		Token:     "tok",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetStatus() != 200 {
		t.Errorf("Status = %d, want 200", resp.GetStatus())
	}
	if resp.GetBody() != `{"operation":"test_op"}` {
		t.Errorf("Body = %q, want %q", resp.GetBody(), `{"operation":"test_op"}`)
	}
}

func TestProviderServerStartProvider(t *testing.T) {
	t.Parallel()

	prov := &startableStubProvider{
		stubProvider: stubProvider{
			name:     "test-provider",
			connMode: gestalt.ConnectionModeNone,
		},
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	cfg, _ := structpb.NewStruct(map[string]any{"key": "val"})
	resp, err := client.StartProvider(ctx, &proto.StartProviderRequest{
		Name:            "my-instance",
		Config:          cfg,
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if resp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", resp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if prov.startName != "my-instance" {
		t.Errorf("startName = %q, want %q", prov.startName, "my-instance")
	}
	if prov.startConfig["key"] != "val" {
		t.Errorf("startConfig[key] = %v, want %q", prov.startConfig["key"], "val")
	}
}

func TestProviderServerStartProviderNoOp(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: gestalt.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	resp, err := client.StartProvider(ctx, &proto.StartProviderRequest{
		Name:            "my-instance",
		ProtocolVersion: proto.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if resp.GetProtocolVersion() != proto.CurrentProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", resp.GetProtocolVersion(), proto.CurrentProtocolVersion)
	}
}

func TestProviderServerConfigSchema(t *testing.T) {
	t.Parallel()

	prov := &schemaStubProvider{
		stubProvider: stubProvider{
			name:     "test-provider",
			connMode: gestalt.ConnectionModeNone,
		},
		schemaDocument: `{"type":"object"}`,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetConfigSchema() != `{"type":"object"}` {
		t.Errorf("ConfigSchema = %q, want %q", meta.GetConfigSchema(), `{"type":"object"}`)
	}
}

func TestProviderServerUnimplementedRPCs(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: gestalt.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	_, err := client.GetSessionCatalog(ctx, &proto.GetSessionCatalogRequest{Token: "t"})
	if err == nil {
		t.Error("GetSessionCatalog should return UNIMPLEMENTED")
	}

	_, err = client.PostConnect(ctx, &proto.PostConnectRequest{})
	if err == nil {
		t.Error("PostConnect should return UNIMPLEMENTED")
	}
}
