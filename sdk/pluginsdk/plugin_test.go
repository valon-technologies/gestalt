package pluginsdk_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type stubProvider struct {
	name        string
	displayName string
	description string
	connMode    pluginsdk.ConnectionMode
	ops         []pluginsdk.Operation
}

func (p *stubProvider) Name() string                             { return p.name }
func (p *stubProvider) DisplayName() string                      { return p.displayName }
func (p *stubProvider) Description() string                      { return p.description }
func (p *stubProvider) ConnectionMode() pluginsdk.ConnectionMode { return p.connMode }
func (p *stubProvider) ListOperations() []pluginsdk.Operation    { return p.ops }

func (p *stubProvider) Execute(_ context.Context, operation string, params map[string]any, _ string) (*pluginsdk.OperationResult, error) {
	return &pluginsdk.OperationResult{
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

type oauthStubProvider struct {
	stubProvider
}

func (p *oauthStubProvider) AuthorizationURL(state string, scopes []string) string {
	return fmt.Sprintf("https://example.com/oauth?state=%s&scope=%d", state, len(scopes))
}

func (p *oauthStubProvider) ExchangeCode(_ context.Context, code string) (*pluginsdk.TokenResponse, error) {
	return &pluginsdk.TokenResponse{
		AccessToken:  "access:" + code,
		RefreshToken: "refresh:" + code,
		ExpiresIn:    3600,
		TokenType:    "Bearer",
		Extra:        map[string]any{"team": "eng"},
	}, nil
}

func (p *oauthStubProvider) RefreshToken(_ context.Context, refreshToken string) (*pluginsdk.TokenResponse, error) {
	return &pluginsdk.TokenResponse{
		AccessToken: "fresh:" + refreshToken,
		TokenType:   "Bearer",
	}, nil
}

type oauthManualStubProvider struct {
	oauthStubProvider
}

func (p *oauthManualStubProvider) SupportsManualAuth() bool { return true }

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
	if meta.GetConnectionMode() != pluginapiv1.ConnectionMode_CONNECTION_MODE_NONE {
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

func TestProviderServerGetMetadata_OAuthAuth(t *testing.T) {
	t.Parallel()

	prov := &oauthStubProvider{
		stubProvider: stubProvider{name: "oauth-prov"},
	}

	client := newProviderPluginClient(t, prov)
	meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	authTypes := meta.GetAuthTypes()
	if len(authTypes) != 1 || authTypes[0] != "oauth" {
		t.Fatalf("AuthTypes = %v, want [oauth]", authTypes)
	}
}

func TestProviderServerGetMetadata_OAuthAndManualAuth(t *testing.T) {
	t.Parallel()

	prov := &oauthManualStubProvider{
		oauthStubProvider: oauthStubProvider{
			stubProvider: stubProvider{name: "oauth-manual-prov"},
		},
	}

	client := newProviderPluginClient(t, prov)
	meta, err := client.GetMetadata(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	authTypes := meta.GetAuthTypes()
	if len(authTypes) != 2 || authTypes[0] != "oauth" || authTypes[1] != "manual" {
		t.Fatalf("AuthTypes = %v, want [oauth manual]", authTypes)
	}
}

func TestProviderServerListOperations(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: pluginsdk.ConnectionModeNone,
		ops: []pluginsdk.Operation{
			{
				Name:        "list_items",
				Description: "List all items",
				Method:      http.MethodGet,
				Parameters: []pluginsdk.Parameter{
					{Name: "limit", Type: "integer", Description: "Max results", Required: false, Default: 10},
				},
			},
			{
				Name:        "create_item",
				Description: "Create a new item",
				Method:      http.MethodPost,
				Parameters: []pluginsdk.Parameter{
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
		connMode: pluginsdk.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	params, _ := structpb.NewStruct(map[string]any{"key": "value"})
	resp, err := client.Execute(ctx, &pluginapiv1.ExecuteRequest{
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
			connMode: pluginsdk.ConnectionModeNone,
		},
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	cfg, _ := structpb.NewStruct(map[string]any{"key": "val"})
	resp, err := client.StartProvider(ctx, &pluginapiv1.StartProviderRequest{
		Name:            "my-instance",
		Config:          cfg,
		ProtocolVersion: pluginapiv1.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if resp.GetProtocolVersion() != pluginapiv1.CurrentProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", resp.GetProtocolVersion(), pluginapiv1.CurrentProtocolVersion)
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
		connMode: pluginsdk.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	resp, err := client.StartProvider(ctx, &pluginapiv1.StartProviderRequest{
		Name:            "my-instance",
		ProtocolVersion: pluginapiv1.CurrentProtocolVersion,
	})
	if err != nil {
		t.Fatalf("StartProvider: %v", err)
	}
	if resp.GetProtocolVersion() != pluginapiv1.CurrentProtocolVersion {
		t.Errorf("ProtocolVersion = %d, want %d", resp.GetProtocolVersion(), pluginapiv1.CurrentProtocolVersion)
	}
}

func TestProviderServerConfigSchema(t *testing.T) {
	t.Parallel()

	prov := &schemaStubProvider{
		stubProvider: stubProvider{
			name:     "test-provider",
			connMode: pluginsdk.ConnectionModeNone,
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
		connMode: pluginsdk.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	meta, err := client.GetMetadata(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if meta.GetMinProtocolVersion() != pluginapiv1.CurrentProtocolVersion {
		t.Errorf("MinProtocolVersion = %d, want %d", meta.GetMinProtocolVersion(), pluginapiv1.CurrentProtocolVersion)
	}
	if meta.GetMaxProtocolVersion() != pluginapiv1.CurrentProtocolVersion {
		t.Errorf("MaxProtocolVersion = %d, want %d", meta.GetMaxProtocolVersion(), pluginapiv1.CurrentProtocolVersion)
	}
}

func TestProviderServerOAuthRPCs(t *testing.T) {
	t.Parallel()

	prov := &oauthStubProvider{
		stubProvider: stubProvider{
			name:     "oauth-provider",
			connMode: pluginsdk.ConnectionModeUser,
		},
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	authResp, err := client.AuthorizationURL(ctx, &pluginapiv1.AuthorizationURLRequest{
		State:  "state-123",
		Scopes: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("AuthorizationURL: %v", err)
	}
	if authResp.GetUrl() != "https://example.com/oauth?state=state-123&scope=2" {
		t.Fatalf("AuthorizationURL = %q", authResp.GetUrl())
	}

	exchangeResp, err := client.ExchangeCode(ctx, &pluginapiv1.ExchangeCodeRequest{Code: "abc"})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if exchangeResp.GetAccessToken() != "access:abc" {
		t.Fatalf("AccessToken = %q", exchangeResp.GetAccessToken())
	}
	if exchangeResp.GetRefreshToken() != "refresh:abc" {
		t.Fatalf("RefreshToken = %q", exchangeResp.GetRefreshToken())
	}
	if exchangeResp.GetExpiresIn() != 3600 {
		t.Fatalf("ExpiresIn = %d", exchangeResp.GetExpiresIn())
	}
	if exchangeResp.GetTokenType() != "Bearer" {
		t.Fatalf("TokenType = %q", exchangeResp.GetTokenType())
	}
	if got := exchangeResp.GetExtra().AsMap()["team"]; got != "eng" {
		t.Fatalf("Extra.team = %v", got)
	}

	refreshResp, err := client.RefreshToken(ctx, &pluginapiv1.RefreshTokenRequest{RefreshToken: "refresh:abc"})
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshResp.GetAccessToken() != "fresh:refresh:abc" {
		t.Fatalf("RefreshToken.AccessToken = %q", refreshResp.GetAccessToken())
	}
}

func TestProviderServerUnimplementedRPCs(t *testing.T) {
	t.Parallel()

	prov := &stubProvider{
		name:     "test-provider",
		connMode: pluginsdk.ConnectionModeNone,
	}

	client := newProviderPluginClient(t, prov)
	ctx := context.Background()

	_, err := client.AuthorizationURL(ctx, &pluginapiv1.AuthorizationURLRequest{State: "s"})
	if err == nil {
		t.Error("AuthorizationURL should return UNIMPLEMENTED")
	}

	_, err = client.ExchangeCode(ctx, &pluginapiv1.ExchangeCodeRequest{Code: "c"})
	if err == nil {
		t.Error("ExchangeCode should return UNIMPLEMENTED")
	}

	_, err = client.RefreshToken(ctx, &pluginapiv1.RefreshTokenRequest{RefreshToken: "r"})
	if err == nil {
		t.Error("RefreshToken should return UNIMPLEMENTED")
	}

	_, err = client.GetSessionCatalog(ctx, &pluginapiv1.GetSessionCatalogRequest{Token: "t"})
	if err == nil {
		t.Error("GetSessionCatalog should return UNIMPLEMENTED")
	}

	_, err = client.PostConnect(ctx, &pluginapiv1.PostConnectRequest{})
	if err == nil {
		t.Error("PostConnect should return UNIMPLEMENTED")
	}
}
