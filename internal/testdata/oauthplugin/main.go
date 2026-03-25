package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/pluginapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := pluginapi.ServeProvider(ctx, &oauthFixtureProvider{}); err != nil {
		log.Fatal(err)
	}
}

type oauthFixtureProvider struct{}

func (p *oauthFixtureProvider) Name() string        { return "exec-oauth" }
func (p *oauthFixtureProvider) DisplayName() string { return "Executable OAuth Fixture" }
func (p *oauthFixtureProvider) Description() string { return "Test-only executable OAuth provider" }
func (p *oauthFixtureProvider) ConnectionMode() core.ConnectionMode {
	return core.ConnectionModeUser
}

func (p *oauthFixtureProvider) ListOperations() []core.Operation {
	return []core.Operation{{
		Name:        "echo_token",
		Description: "Echo the token used for invocation",
		Method:      "GET",
	}}
}

func (p *oauthFixtureProvider) Execute(ctx context.Context, _ string, _ map[string]any, token string) (*core.OperationResult, error) {
	body, err := json.Marshal(map[string]string{
		"token":  token,
		"tenant": core.ConnectionParams(ctx)["tenant"],
	})
	if err != nil {
		return nil, err
	}
	return &core.OperationResult{Status: 200, Body: string(body)}, nil
}

func (p *oauthFixtureProvider) AuthorizationURL(state string, scopes []string) string {
	return fmt.Sprintf("https://default.example.com/oauth?state=%s&scope=%d", url.QueryEscape(state), len(scopes))
}

func (p *oauthFixtureProvider) StartOAuth(state string, scopes []string) (string, string) {
	return p.AuthorizationURL(state, scopes), "fixture-verifier"
}

func (p *oauthFixtureProvider) AuthorizationBaseURL() string {
	return "https://{tenant}.example.com/oauth"
}

func (p *oauthFixtureProvider) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return fmt.Sprintf("%s?state=%s&scope=%d", authBaseURL, url.QueryEscape(state), len(scopes)), "fixture-verifier"
}

func (p *oauthFixtureProvider) TokenURL() string {
	return "https://{tenant}.example.com/token"
}

func (p *oauthFixtureProvider) ExchangeCode(_ context.Context, code string) (*core.TokenResponse, error) {
	return &core.TokenResponse{
		AccessToken:  "access:" + code,
		RefreshToken: "refresh:" + code,
		TokenType:    "Bearer",
	}, nil
}

func (p *oauthFixtureProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	opts := oauth.ResolveExchangeOptions(extraOpts...)
	tenant := core.ConnectionParams(ctx)["tenant"]
	return &core.TokenResponse{
		AccessToken:  fmt.Sprintf("access:%s|%s|%s|%s", code, tenant, verifier, opts.TokenURL),
		RefreshToken: "refresh:" + tenant,
		TokenType:    "Bearer",
	}, nil
}

func (p *oauthFixtureProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	tenant := core.ConnectionParams(ctx)["tenant"]
	return &core.TokenResponse{
		AccessToken: fmt.Sprintf("fresh:%s|%s", tenant, refreshToken),
		TokenType:   "Bearer",
	}, nil
}

func (p *oauthFixtureProvider) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	tenant := core.ConnectionParams(ctx)["tenant"]
	return &core.TokenResponse{
		AccessToken: fmt.Sprintf("fresh:%s|%s|%s", tenant, refreshToken, tokenURL),
		TokenType:   "Bearer",
	}, nil
}

func (p *oauthFixtureProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return map[string]core.ConnectionParamDef{
		"tenant": {Required: true, Description: "Tenant slug"},
	}
}
