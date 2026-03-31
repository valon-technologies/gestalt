package gestalt_test

import (
	"context"
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
	ops         []gestalt.Operation
}

func (p *stubProvider) Name() string                             { return p.name }
func (p *stubProvider) DisplayName() string                      { return p.displayName }
func (p *stubProvider) Description() string                      { return p.description }
func (p *stubProvider) ConnectionMode() gestalt.ConnectionMode { return p.connMode }
func (p *stubProvider) ListOperations() []gestalt.Operation    { return p.ops }

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
	schema string
}

func (p *schemaStubProvider) ConfigSchemaJSON() string { return p.schema }

type manualAuthStubProvider struct {
	stubProvider
}

func (p *manualAuthStubProvider) SupportsManualAuth() bool { return true }


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


func TestProviderServerListOperations(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: gestalt.ConnectionModeNone,
		ops: []gestalt.Operation{
			{
				Name:        "list_items",
				Description: "List all items",
				Method:      http.MethodGet,
				Parameters: []gestalt.Parameter{
					{Name: "limit", Type: "integer", Description: "Max results", Required: false, Default: 10},
				},
			},
			{
				Name:        "create_item",
				Description: "Create a new item",
				Method:      http.MethodPost,
				Parameters: []gestalt.Parameter{
					{Name: "name", Type: "string", Description: "Item name", Required: true},
				},
			},
		},
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	resp, err := client.ListOperations(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("ListOperations: %v", err)
	}
	if len(resp.GetOperations()) != 2 {
		t.Fatalf("len(Operations) = %d, want 2", len(resp.GetOperations()))
	}

	op := resp.GetOperations()[0]
	if op.GetName() != "list_items" {
		t.Errorf("Operations[0].Name = %q, want %q", op.GetName(), "list_items")
	}
	if len(op.GetParameters()) != 1 {
		t.Fatalf("len(Operations[0].Parameters) = %d, want 1", len(op.GetParameters()))
	}
	param := op.GetParameters()[0]
	if param.GetName() != "limit" {
		t.Errorf("param.Name = %q, want %q", param.GetName(), "limit")
	}
	if param.GetRequired() {
		t.Errorf("param.Required = true, want false")
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
		schema: `{"type":"object"}`,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetConfigSchemaJson() != `{"type":"object"}` {
		t.Errorf("ConfigSchemaJson = %q, want %q", meta.GetConfigSchemaJson(), `{"type":"object"}`)
	}
}

func TestProviderServerMetadataProtocolVersions(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: gestalt.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetMinProtocolVersion() != proto.CurrentProtocolVersion {
		t.Errorf("MinProtocolVersion = %d, want %d", meta.GetMinProtocolVersion(), proto.CurrentProtocolVersion)
	}
	if meta.GetMaxProtocolVersion() != proto.CurrentProtocolVersion {
		t.Errorf("MaxProtocolVersion = %d, want %d", meta.GetMaxProtocolVersion(), proto.CurrentProtocolVersion)
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
