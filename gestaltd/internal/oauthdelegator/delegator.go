package oauthdelegator

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
)

type Handler interface {
	AuthorizationURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
}

// Delegator implements all OAuth-related methods by delegating to a base
// handler. Callers embed this to avoid duplicating the optional PKCE, token
// URL, and auth base URL forwarding logic.
type Delegator struct {
	Target Handler
}

type UpstreamAdapter struct {
	*oauth.UpstreamHandler
	delegator Delegator
}

type upstreamHandlerTarget struct {
	*oauth.UpstreamHandler
}

func (t upstreamHandlerTarget) AuthorizationURL(state string, scopes []string) string {
	return t.UpstreamHandler.AuthorizationURL(state, scopes)
}

func (t upstreamHandlerTarget) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return t.UpstreamHandler.ExchangeCode(ctx, code)
}

func (t upstreamHandlerTarget) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return t.UpstreamHandler.RefreshToken(ctx, refreshToken)
}

func NewUpstreamAdapter(handler *oauth.UpstreamHandler) UpstreamAdapter {
	return UpstreamAdapter{
		UpstreamHandler: handler,
		delegator:       Delegator{Target: upstreamHandlerTarget{UpstreamHandler: handler}},
	}
}

func (a UpstreamAdapter) AuthorizationURL(state string, scopes []string) string {
	return a.delegator.AuthorizationURL(state, scopes)
}

func (a UpstreamAdapter) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return a.delegator.ExchangeCode(ctx, code)
}

func (a UpstreamAdapter) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return a.delegator.RefreshToken(ctx, refreshToken)
}

func (a UpstreamAdapter) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	return a.delegator.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
}

func (a UpstreamAdapter) AuthorizationBaseURL() string {
	return a.delegator.AuthorizationBaseURL()
}

func (a UpstreamAdapter) TokenURL() string {
	return a.delegator.TokenURL()
}

func (d Delegator) AuthorizationURL(state string, scopes []string) string {
	return d.Target.AuthorizationURL(state, scopes)
}

func (d Delegator) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return d.Target.ExchangeCode(ctx, code)
}

func (d Delegator) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return d.Target.RefreshToken(ctx, refreshToken)
}

func (d Delegator) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	type refresher interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}
	if rw, ok := d.Target.(refresher); ok {
		return rw.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
	}
	return d.Target.RefreshToken(ctx, refreshToken)
}

func (d Delegator) StartOAuth(state string, scopes []string) (string, string) {
	type starter interface {
		StartOAuth(state string, scopes []string) (string, string)
	}
	if s, ok := d.Target.(starter); ok {
		return s.StartOAuth(state, scopes)
	}
	return d.Target.AuthorizationURL(state, scopes), ""
}

func (d Delegator) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	type overrider interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}
	if ov, ok := d.Target.(overrider); ok {
		return ov.StartOAuthWithOverride(authBaseURL, state, scopes)
	}
	return d.Target.AuthorizationURL(state, scopes), ""
}

func (d Delegator) AuthorizationBaseURL() string {
	type authBaseURLer interface{ AuthorizationBaseURL() string }
	if abu, ok := d.Target.(authBaseURLer); ok {
		return abu.AuthorizationBaseURL()
	}
	return ""
}

func (d Delegator) TokenURL() string {
	type tokenURLer interface{ TokenURL() string }
	if tu, ok := d.Target.(tokenURLer); ok {
		return tu.TokenURL()
	}
	return ""
}

func (d Delegator) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	type exchanger interface {
		ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	}
	if e, ok := d.Target.(exchanger); ok {
		return e.ExchangeCodeWithVerifier(ctx, code, verifier, extraOpts...)
	}
	return d.Target.ExchangeCode(ctx, code)
}

func (a UpstreamAdapter) StartOAuth(state string, scopes []string) (string, string) {
	return a.UpstreamHandler.AuthorizationURLWithPKCE(state, scopes)
}

func (a UpstreamAdapter) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return a.UpstreamHandler.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

func (a UpstreamAdapter) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	opts = append(opts, extraOpts...)
	return a.UpstreamHandler.ExchangeCode(ctx, code, opts...)
}
