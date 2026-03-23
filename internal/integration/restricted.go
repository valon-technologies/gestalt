package integration

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/oauth"
)

// Restricted wraps a Provider to expose only a subset of its operations.
type Restricted struct {
	inner   core.Provider
	allowed map[string]struct{}
}

// Compile-time interface checks.
var (
	_ core.Provider            = (*Restricted)(nil)
	_ core.ManualProvider      = (*Restricted)(nil)
	_ core.CatalogProvider     = (*Restricted)(nil)
	_ core.PostConnectProvider = (*Restricted)(nil)
	_ core.OAuthProvider       = (*restrictedOAuth)(nil)
	_ core.ManualProvider      = (*restrictedOAuth)(nil)
)

// NewRestricted returns a Provider that gates operations to the allowed set.
// If the inner provider implements OAuthProvider, the returned value does too.
func NewRestricted(inner core.Provider, allowed []string) core.Provider {
	m := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		m[name] = struct{}{}
	}
	r := &Restricted{inner: inner, allowed: m}
	if oauth, ok := inner.(core.OAuthProvider); ok {
		return &restrictedOAuth{Restricted: r, oauth: oauth}
	}
	return r
}

func (r *Restricted) Name() string                        { return r.inner.Name() }
func (r *Restricted) DisplayName() string                 { return r.inner.DisplayName() }
func (r *Restricted) Description() string                 { return r.inner.Description() }
func (r *Restricted) ConnectionMode() core.ConnectionMode { return r.inner.ConnectionMode() }

func (r *Restricted) ListOperations() []core.Operation {
	all := r.inner.ListOperations()
	filtered := make([]core.Operation, 0, len(r.allowed))
	for _, op := range all {
		if _, ok := r.allowed[op.Name]; ok {
			filtered = append(filtered, op)
		}
	}
	return filtered
}

func (r *Restricted) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if _, ok := r.allowed[operation]; !ok {
		return nil, fmt.Errorf("operation %q is not allowed", operation)
	}
	return r.inner.Execute(ctx, operation, params, token)
}

func (r *Restricted) SupportsManualAuth() bool {
	if mp, ok := r.inner.(core.ManualProvider); ok {
		return mp.SupportsManualAuth()
	}
	return false
}

func (r *Restricted) Catalog() *catalog.Catalog {
	cp, ok := r.inner.(core.CatalogProvider)
	if !ok {
		return nil
	}
	cat := cp.Catalog()
	if cat == nil {
		return nil
	}
	filtered := *cat
	filtered.Operations = nil
	for i := range cat.Operations {
		if _, ok := r.allowed[cat.Operations[i].ID]; ok {
			filtered.Operations = append(filtered.Operations, cat.Operations[i])
		}
	}
	return &filtered
}

// Inner returns the unwrapped provider.
func (r *Restricted) Inner() core.Provider {
	return r.inner
}

// restrictedOAuth wraps a Restricted provider and delegates OAuth methods
// to the inner OAuthProvider.
type restrictedOAuth struct {
	*Restricted
	oauth core.OAuthProvider
}

func (r *restrictedOAuth) AuthorizationURL(state string, scopes []string) string {
	return r.oauth.AuthorizationURL(state, scopes)
}

func (r *restrictedOAuth) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return r.oauth.ExchangeCode(ctx, code)
}

func (r *restrictedOAuth) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return r.oauth.RefreshToken(ctx, refreshToken)
}

func (r *restrictedOAuth) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	type refresher interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}
	if rw, ok := r.oauth.(refresher); ok {
		return rw.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
	}
	return r.oauth.RefreshToken(ctx, refreshToken)
}

type oauthVerifierExchanger interface {
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
}

func (r *restrictedOAuth) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	if exchanger, ok := r.oauth.(oauthVerifierExchanger); ok {
		return exchanger.ExchangeCodeWithVerifier(ctx, code, verifier, extraOpts...)
	}
	return r.oauth.ExchangeCode(ctx, code)
}

func (r *restrictedOAuth) TokenURL() string {
	type tokenURLer interface{ TokenURL() string }
	if tu, ok := r.oauth.(tokenURLer); ok {
		return tu.TokenURL()
	}
	return ""
}

func (r *restrictedOAuth) AuthorizationBaseURL() string {
	type authBaseURLer interface{ AuthorizationBaseURL() string }
	if abu, ok := r.oauth.(authBaseURLer); ok {
		return abu.AuthorizationBaseURL()
	}
	return ""
}

func (r *restrictedOAuth) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	type overrider interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}
	if ov, ok := r.oauth.(overrider); ok {
		return ov.StartOAuthWithOverride(authBaseURL, state, scopes)
	}
	return r.oauth.AuthorizationURL(state, scopes), ""
}

func (r *Restricted) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	if cpp, ok := r.inner.(core.ConnectionParamProvider); ok {
		return cpp.ConnectionParamDefs()
	}
	return nil
}

func (r *Restricted) PostConnectHook() core.PostConnectHook {
	if pcp, ok := r.inner.(core.PostConnectProvider); ok {
		return pcp.PostConnectHook()
	}
	return nil
}
