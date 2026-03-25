package composite

import (
	"context"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/oauth"
)

// oauthDelegator implements all OAuth-related methods by delegating to a
// core.OAuthProvider. Both oauthProvider and overlayOAuthProvider embed
// this to avoid duplicating the delegation logic.
type oauthDelegator struct {
	oauth core.OAuthProvider
}

func (d *oauthDelegator) AuthorizationURL(state string, scopes []string) string {
	return d.oauth.AuthorizationURL(state, scopes)
}

func (d *oauthDelegator) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return d.oauth.ExchangeCode(ctx, code)
}

func (d *oauthDelegator) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return d.oauth.RefreshToken(ctx, refreshToken)
}

func (d *oauthDelegator) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	type refresher interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}
	if rw, ok := d.oauth.(refresher); ok {
		return rw.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
	}
	return d.oauth.RefreshToken(ctx, refreshToken)
}

func (d *oauthDelegator) StartOAuth(state string, scopes []string) (string, string) {
	type starter interface {
		StartOAuth(state string, scopes []string) (string, string)
	}
	if s, ok := d.oauth.(starter); ok {
		return s.StartOAuth(state, scopes)
	}
	return d.oauth.AuthorizationURL(state, scopes), ""
}

func (d *oauthDelegator) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	type overrider interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}
	if ov, ok := d.oauth.(overrider); ok {
		return ov.StartOAuthWithOverride(authBaseURL, state, scopes)
	}
	return d.oauth.AuthorizationURL(state, scopes), ""
}

func (d *oauthDelegator) AuthorizationBaseURL() string {
	type authBaseURLer interface{ AuthorizationBaseURL() string }
	if abu, ok := d.oauth.(authBaseURLer); ok {
		return abu.AuthorizationBaseURL()
	}
	return ""
}

func (d *oauthDelegator) TokenURL() string {
	type tokenURLer interface{ TokenURL() string }
	if tu, ok := d.oauth.(tokenURLer); ok {
		return tu.TokenURL()
	}
	return ""
}

func (d *oauthDelegator) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	type exchanger interface {
		ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
	}
	if e, ok := d.oauth.(exchanger); ok {
		return e.ExchangeCodeWithVerifier(ctx, code, verifier, extraOpts...)
	}
	return d.oauth.ExchangeCode(ctx, code)
}
