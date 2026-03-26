package pluginapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type roundTripProvider struct{}

func (p *roundTripProvider) Name() string        { return "roundtrip" }
func (p *roundTripProvider) DisplayName() string { return "Round Trip" }
func (p *roundTripProvider) Description() string { return "test provider" }
func (p *roundTripProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeEither
}

func (p *roundTripProvider) ListOperations() []core.Operation {
	return []core.Operation{
		{
			Name:        "echo",
			Description: "Echo input",
			Method:      "POST",
			Parameters: []core.Parameter{
				{Name: "message", Type: "string", Description: "message", Required: true, Default: "hello"},
			},
		},
	}
}

func (p *roundTripProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return &core.OperationResult{
		Status: 201,
		Body:   fmt.Sprintf("%s|%s|%s|%s", operation, token, params["message"], core.ConnectionParams(ctx)["tenant"]),
	}, nil
}

func (p *roundTripProvider) AuthorizationURL(state string, scopes []string) string {
	return fmt.Sprintf("https://example.com/oauth?state=%s&scope=%d", state, len(scopes))
}

func (p *roundTripProvider) ExchangeCode(_ context.Context, code string) (*core.TokenResponse, error) {
	return &core.TokenResponse{
		AccessToken:  "access:" + code,
		RefreshToken: "refresh:" + code,
		ExpiresIn:    3600,
		TokenType:    "Bearer",
		Extra:        map[string]any{"tenant": "acme"},
	}, nil
}

func (p *roundTripProvider) RefreshToken(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
	return &core.TokenResponse{
		AccessToken: "fresh:" + refreshToken,
		TokenType:   "Bearer",
	}, nil
}

func (p *roundTripProvider) SupportsManualAuth() bool { return true }

func (p *roundTripProvider) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        "roundtrip",
		DisplayName: "Round Trip",
		Description: "test provider",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: "POST", Path: "/echo", Transport: catalog.TransportREST},
		},
	}
}

func (p *roundTripProvider) CatalogForRequest(_ context.Context, token string) (*catalog.Catalog, error) {
	return &catalog.Catalog{
		Name:        "roundtrip-session",
		DisplayName: token,
		Description: "session catalog",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: "POST", Path: "/echo", Transport: catalog.TransportREST},
		},
	}, nil
}

func (p *roundTripProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return map[string]core.ConnectionParamDef{
		"tenant":  {Required: true, Description: "Tenant slug"},
		"team_id": {From: "token_response", Field: "team_id"},
	}
}

func (p *roundTripProvider) AuthTypes() []string {
	return []string{"oauth", "manual"}
}

func TestRemoteProviderRoundTrip(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil, "")
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	if prov.Name() != "roundtrip" {
		t.Fatalf("unexpected provider name: %q", prov.Name())
	}
	if prov.DisplayName() != "Round Trip" {
		t.Fatalf("unexpected display name: %q", prov.DisplayName())
	}
	if prov.ConnectionMode() != core.ConnectionModeEither {
		t.Fatalf("unexpected connection mode: %q", prov.ConnectionMode())
	}

	if _, ok := prov.(core.OAuthProvider); !ok {
		t.Fatal("expected remote provider to implement OAuthProvider")
	}
	if _, ok := prov.(core.ManualProvider); !ok {
		t.Fatal("expected remote provider to implement ManualProvider")
	}
	if _, ok := prov.(core.CatalogProvider); !ok {
		t.Fatal("expected remote provider to implement CatalogProvider")
	}
	if _, ok := prov.(core.SessionCatalogProvider); !ok {
		t.Fatal("expected remote provider to implement SessionCatalogProvider")
	}
	if _, ok := prov.(core.ConnectionParamProvider); !ok {
		t.Fatal("expected remote provider to implement ConnectionParamProvider")
	}
	if _, ok := prov.(core.AuthTypeLister); !ok {
		t.Fatal("expected remote provider to implement AuthTypeLister")
	}

	ctx := core.WithConnectionParams(context.Background(), map[string]string{"tenant": "acme"})
	result, err := prov.Execute(ctx, "echo", map[string]any{"message": "hi"}, "secret-token")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 201 || result.Body != "echo|secret-token|hi|acme" {
		t.Fatalf("unexpected execute result: %+v", result)
	}

	oauthProv := prov.(core.OAuthProvider)
	if got := oauthProv.AuthorizationURL("state-123", []string{"read", "write"}); got != "https://example.com/oauth?state=state-123&scope=2" {
		t.Fatalf("unexpected authorization url: %q", got)
	}
	tok, err := oauthProv.ExchangeCode(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "access:abc" || tok.RefreshToken != "refresh:abc" || tok.Extra["tenant"] != "acme" {
		t.Fatalf("unexpected exchange token response: %+v", tok)
	}

	cp := prov.(core.CatalogProvider)
	if cat := cp.Catalog(); cat == nil || cat.Name != "roundtrip" {
		t.Fatalf("unexpected static catalog: %+v", cat)
	}

	scp := prov.(core.SessionCatalogProvider)
	sessionCat, err := scp.CatalogForRequest(context.Background(), "token-123")
	if err != nil {
		t.Fatalf("CatalogForRequest: %v", err)
	}
	if sessionCat.Name != "roundtrip-session" || sessionCat.DisplayName != "token-123" {
		t.Fatalf("unexpected session catalog: %+v", sessionCat)
	}

	cpp := prov.(core.ConnectionParamProvider)
	if defs := cpp.ConnectionParamDefs(); defs["tenant"].Description != "Tenant slug" || defs["team_id"].Field != "team_id" {
		t.Fatalf("unexpected connection param defs: %+v", defs)
	}

}

type manualOnlyProviderServer struct {
	pluginapiv1.UnimplementedProviderPluginServer
}

func (s *manualOnlyProviderServer) StartProvider(_ context.Context, _ *pluginapiv1.StartProviderRequest) (*pluginapiv1.StartProviderResponse, error) {
	return &pluginapiv1.StartProviderResponse{ProtocolVersion: pluginapiv1.CurrentProtocolVersion}, nil
}

func (s *manualOnlyProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ProviderMetadata, error) {
	return &pluginapiv1.ProviderMetadata{
		Name:               "manual-only",
		DisplayName:        "Manual Only",
		ConnectionMode:     pluginapiv1.ConnectionMode_CONNECTION_MODE_IDENTITY,
		AuthTypes:          []string{"manual"},
		MinProtocolVersion: pluginapiv1.CurrentProtocolVersion,
		MaxProtocolVersion: pluginapiv1.CurrentProtocolVersion,
	}, nil
}

func (s *manualOnlyProviderServer) ListOperations(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ListOperationsResponse, error) {
	return &pluginapiv1.ListOperationsResponse{}, nil
}

func (s *manualOnlyProviderServer) Execute(_ context.Context, _ *pluginapiv1.ExecuteRequest) (*pluginapiv1.OperationResult, error) {
	return &pluginapiv1.OperationResult{Status: 200, Body: `{}`}, nil
}

func (s *manualOnlyProviderServer) AuthorizationURL(_ context.Context, _ *pluginapiv1.AuthorizationURLRequest) (*pluginapiv1.AuthorizationURLResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not supported")
}

func TestRemoteProviderManualAuthOnly(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, &manualOnlyProviderServer{})
	prov, err := NewRemoteProvider(context.Background(), client, "manual-only", nil, "")
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	mp, ok := prov.(core.ManualProvider)
	if !ok {
		t.Fatal("expected remote provider to implement core.ManualProvider")
	}
	if !mp.SupportsManualAuth() {
		t.Fatal("expected SupportsManualAuth() == true")
	}

	if _, ok := prov.(core.OAuthProvider); ok {
		t.Fatal("expected remote provider to NOT implement core.OAuthProvider")
	}
}
