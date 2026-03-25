package pluginapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/oauth"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type roundTripProvider struct {
	gotAuthBaseURL      string
	gotExchangeVerifier string
	gotExchangeTokenURL string
	gotExchangeTenant   string
	gotRefreshTokenURL  string
	gotRefreshTenant    string
}

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

func (p *roundTripProvider) StartOAuth(state string, scopes []string) (string, string) {
	return fmt.Sprintf("https://default.example.com/oauth?state=%s&scope=%d", state, len(scopes)), "verifier:" + state
}

func (p *roundTripProvider) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	p.gotAuthBaseURL = authBaseURL
	return fmt.Sprintf("%s?state=%s&scope=%d", authBaseURL, state, len(scopes)), "override-verifier:" + state
}

func (p *roundTripProvider) AuthorizationBaseURL() string {
	return "https://{tenant}.example.com/oauth"
}

func (p *roundTripProvider) TokenURL() string {
	return "https://{tenant}.example.com/token"
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

func (p *roundTripProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	resolved := oauth.ResolveExchangeOptions(extraOpts...)
	p.gotExchangeVerifier = verifier
	p.gotExchangeTokenURL = resolved.TokenURL
	p.gotExchangeTenant = core.ConnectionParams(ctx)["tenant"]
	return &core.TokenResponse{
		AccessToken:  "access-with-verifier:" + code,
		RefreshToken: "refresh-with-verifier:" + code,
		TokenType:    "Bearer",
		Extra:        map[string]any{"tenant": p.gotExchangeTenant},
	}, nil
}

func (p *roundTripProvider) RefreshToken(_ context.Context, refreshToken string) (*core.TokenResponse, error) {
	return &core.TokenResponse{
		AccessToken: "fresh:" + refreshToken,
		TokenType:   "Bearer",
	}, nil
}

func (p *roundTripProvider) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	p.gotRefreshTokenURL = tokenURL
	p.gotRefreshTenant = core.ConnectionParams(ctx)["tenant"]
	return &core.TokenResponse{
		AccessToken: "fresh-with-url:" + refreshToken,
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
			{ID: "echo", Method: "POST", Path: "/echo", Transport: catalog.TransportHTTP},
		},
	}
}

func (p *roundTripProvider) CatalogForRequest(_ context.Context, token string) (*catalog.Catalog, error) {
	return &catalog.Catalog{
		Name:        "roundtrip-session",
		DisplayName: token,
		Description: "session catalog",
		Operations: []catalog.CatalogOperation{
			{ID: "echo", Method: "POST", Path: "/echo", Transport: catalog.TransportHTTP},
		},
	}, nil
}

func (p *roundTripProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return map[string]core.ConnectionParamDef{
		"tenant":  {Required: true, Description: "Tenant slug"},
		"team_id": {From: "token_response", Field: "team_id"},
	}
}

func (p *roundTripProvider) PostConnectHook() core.PostConnectHook {
	return func(_ context.Context, tok *core.IntegrationToken, _ *http.Client) (map[string]string, error) {
		return map[string]string{"instance": tok.Instance, "integration": tok.Integration}, nil
	}
}

func (p *roundTripProvider) AuthTypes() []string {
	return []string{"oauth", "manual"}
}

func TestRemoteProviderRoundTrip(t *testing.T) {
	t.Parallel()

	backing := &roundTripProvider{}
	client := newProviderTestClient(t, backing)
	prov, err := NewRemoteProvider(context.Background(), client)
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
	if _, ok := prov.(core.PostConnectProvider); !ok {
		t.Fatal("expected remote provider to implement PostConnectProvider")
	}
	if _, ok := prov.(core.AuthTypeLister); !ok {
		t.Fatal("expected remote provider to implement AuthTypeLister")
	}
	if _, ok := prov.(interface {
		StartOAuth(state string, scopes []string) (string, string)
	}); !ok {
		t.Fatal("expected remote provider to implement PKCE-aware StartOAuth")
	}
	if _, ok := prov.(interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}); !ok {
		t.Fatal("expected remote provider to implement StartOAuthWithOverride")
	}
	if _, ok := prov.(interface{ AuthorizationBaseURL() string }); !ok {
		t.Fatal("expected remote provider to expose AuthorizationBaseURL")
	}
	if _, ok := prov.(interface{ TokenURL() string }); !ok {
		t.Fatal("expected remote provider to expose TokenURL")
	}
	if _, ok := prov.(interface {
		ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	}); !ok {
		t.Fatal("expected remote provider to implement ExchangeCodeWithVerifier")
	}
	if _, ok := prov.(interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}); !ok {
		t.Fatal("expected remote provider to implement RefreshTokenWithURL")
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

	starter := prov.(interface {
		StartOAuth(state string, scopes []string) (string, string)
	})
	startURL, verifier := starter.StartOAuth("state-456", []string{"read"})
	if startURL != "https://default.example.com/oauth?state=state-456&scope=1" || verifier != "verifier:state-456" {
		t.Fatalf("unexpected start oauth response: url=%q verifier=%q", startURL, verifier)
	}

	overrider := prov.(interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	})
	overrideURL, overrideVerifier := overrider.StartOAuthWithOverride("https://acme.example.com/oauth", "state-789", []string{"read", "write"})
	if overrideURL != "https://acme.example.com/oauth?state=state-789&scope=2" || overrideVerifier != "override-verifier:state-789" {
		t.Fatalf("unexpected override oauth response: url=%q verifier=%q", overrideURL, overrideVerifier)
	}
	if backing.gotAuthBaseURL != "https://acme.example.com/oauth" {
		t.Fatalf("got auth base override %q, want override URL", backing.gotAuthBaseURL)
	}

	if got := prov.(interface{ AuthorizationBaseURL() string }).AuthorizationBaseURL(); got != "https://{tenant}.example.com/oauth" {
		t.Fatalf("unexpected authorization base URL: %q", got)
	}
	if got := prov.(interface{ TokenURL() string }).TokenURL(); got != "https://{tenant}.example.com/token" {
		t.Fatalf("unexpected token URL: %q", got)
	}

	verifierExchanger := prov.(interface {
		ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	})
	advancedTok, err := verifierExchanger.ExchangeCodeWithVerifier(ctx, "code-123", "pkce-123", oauth.WithTokenURL("https://acme.example.com/token"))
	if err != nil {
		t.Fatalf("ExchangeCodeWithVerifier: %v", err)
	}
	if advancedTok.AccessToken != "access-with-verifier:code-123" || backing.gotExchangeVerifier != "pkce-123" || backing.gotExchangeTokenURL != "https://acme.example.com/token" || backing.gotExchangeTenant != "acme" {
		t.Fatalf("unexpected advanced exchange result: token=%+v verifier=%q tokenURL=%q tenant=%q", advancedTok, backing.gotExchangeVerifier, backing.gotExchangeTokenURL, backing.gotExchangeTenant)
	}

	urlRefresher := prov.(interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	})
	refreshed, err := urlRefresher.RefreshTokenWithURL(ctx, "refresh-123", "https://acme.example.com/token")
	if err != nil {
		t.Fatalf("RefreshTokenWithURL: %v", err)
	}
	if refreshed.AccessToken != "fresh-with-url:refresh-123" || backing.gotRefreshTokenURL != "https://acme.example.com/token" || backing.gotRefreshTenant != "acme" {
		t.Fatalf("unexpected advanced refresh result: token=%+v tokenURL=%q tenant=%q", refreshed, backing.gotRefreshTokenURL, backing.gotRefreshTenant)
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

	pcp := prov.(core.PostConnectProvider)
	meta, err := pcp.PostConnectHook()(context.Background(), &core.IntegrationToken{
		Integration: "roundtrip",
		Instance:    "default",
	}, nil)
	if err != nil {
		t.Fatalf("PostConnectHook: %v", err)
	}
	if meta["instance"] != "default" || meta["integration"] != "roundtrip" {
		t.Fatalf("unexpected post-connect metadata: %+v", meta)
	}
}

func newProviderTestClient(t *testing.T, prov core.Provider) pluginapiv1.ProviderPluginClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pluginapiv1.RegisterProviderPluginServer(srv, NewProviderServer(prov))
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pluginapiv1.NewProviderPluginClient(conn)
}
