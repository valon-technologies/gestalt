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
