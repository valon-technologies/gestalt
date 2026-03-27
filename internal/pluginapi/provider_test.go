package pluginapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	sdkpluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
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

type manualOnlySDKProvider struct{}

func (p *manualOnlySDKProvider) Name() string { return "manual-only" }

func (p *manualOnlySDKProvider) DisplayName() string { return "Manual Only" }

func (p *manualOnlySDKProvider) Description() string { return "manual auth provider" }

func (p *manualOnlySDKProvider) ConnectionMode() sdkpluginsdk.ConnectionMode {
	return sdkpluginsdk.ConnectionModeIdentity
}

func (p *manualOnlySDKProvider) ListOperations() []sdkpluginsdk.Operation { return nil }

func (p *manualOnlySDKProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*sdkpluginsdk.OperationResult, error) {
	return &sdkpluginsdk.OperationResult{Status: 200, Body: `{}`}, nil
}

func (p *manualOnlySDKProvider) SupportsManualAuth() bool { return true }

type oauthOnlySDKProvider struct{}

func (p *oauthOnlySDKProvider) Name() string { return "oauth-only" }

func (p *oauthOnlySDKProvider) DisplayName() string { return "OAuth Only" }

func (p *oauthOnlySDKProvider) Description() string { return "oauth provider" }

func (p *oauthOnlySDKProvider) ConnectionMode() sdkpluginsdk.ConnectionMode {
	return sdkpluginsdk.ConnectionModeUser
}

func (p *oauthOnlySDKProvider) ListOperations() []sdkpluginsdk.Operation { return nil }

func (p *oauthOnlySDKProvider) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*sdkpluginsdk.OperationResult, error) {
	return &sdkpluginsdk.OperationResult{Status: 200, Body: `{}`}, nil
}

func (p *oauthOnlySDKProvider) AuthorizationURL(state string, scopes []string) string {
	return fmt.Sprintf("https://example.com/sdk-oauth?state=%s&scope=%d", state, len(scopes))
}

func (p *oauthOnlySDKProvider) ExchangeCode(_ context.Context, code string) (*sdkpluginsdk.TokenResponse, error) {
	return &sdkpluginsdk.TokenResponse{
		AccessToken:  "sdk-access:" + code,
		RefreshToken: "sdk-refresh:" + code,
		ExpiresIn:    1800,
		TokenType:    "Bearer",
		Extra:        map[string]any{"workspace": "acme"},
	}, nil
}

func (p *oauthOnlySDKProvider) RefreshToken(_ context.Context, refreshToken string) (*sdkpluginsdk.TokenResponse, error) {
	return &sdkpluginsdk.TokenResponse{
		AccessToken: "sdk-fresh:" + refreshToken,
		TokenType:   "Bearer",
	}, nil
}

func TestRemoteProviderRoundTrip(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, NewProviderServer(&roundTripProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, "roundtrip", nil)
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

func TestRemoteProviderManualAuthOnly(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, sdkpluginsdk.NewProviderServer(&manualOnlySDKProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, "manual-only", nil)
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

func TestRemoteProviderOAuthOnlySDK(t *testing.T) {
	t.Parallel()

	client := newProviderPluginClient(t, sdkpluginsdk.NewProviderServer(&oauthOnlySDKProvider{}))
	prov, err := NewRemoteProvider(context.Background(), client, "oauth-only", nil)
	if err != nil {
		t.Fatalf("NewRemoteProvider: %v", err)
	}

	if prov.Name() != "oauth-only" {
		t.Fatalf("unexpected provider name: %q", prov.Name())
	}
	if prov.ConnectionMode() != core.ConnectionModeUser {
		t.Fatalf("unexpected connection mode: %q", prov.ConnectionMode())
	}

	oauthProv, ok := prov.(core.OAuthProvider)
	if !ok {
		t.Fatal("expected remote provider to implement core.OAuthProvider")
	}

	authTypes, ok := prov.(core.AuthTypeLister)
	if !ok {
		t.Fatal("expected remote provider to implement core.AuthTypeLister")
	}
	if got := authTypes.AuthTypes(); len(got) != 1 || got[0] != "oauth" {
		t.Fatalf("unexpected auth types: %v", got)
	}

	mp, ok := prov.(core.ManualProvider)
	if !ok {
		t.Fatal("expected remote provider to implement core.ManualProvider")
	}
	if mp.SupportsManualAuth() {
		t.Fatal("expected SupportsManualAuth() == false")
	}

	if got := oauthProv.AuthorizationURL("state-xyz", []string{"read", "write"}); got != "https://example.com/sdk-oauth?state=state-xyz&scope=2" {
		t.Fatalf("unexpected authorization url: %q", got)
	}

	tok, err := oauthProv.ExchangeCode(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "sdk-access:abc" || tok.RefreshToken != "sdk-refresh:abc" || tok.Extra["workspace"] != "acme" {
		t.Fatalf("unexpected exchange token response: %+v", tok)
	}

	fresh, err := oauthProv.RefreshToken(context.Background(), "sdk-refresh:abc")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if fresh.AccessToken != "sdk-fresh:sdk-refresh:abc" {
		t.Fatalf("unexpected refresh token response: %+v", fresh)
	}
}
