package integration

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
)

// Restricted wraps a Provider to expose only a subset of its operations,
// optionally renaming them via aliases.
type Restricted struct {
	inner        core.Provider
	allowed      map[string]struct{}
	aliases      map[string]string
	reverseAlias map[string]string
	allowedInner map[string]struct{}
	descriptions map[string]string
}

// RestrictedOption configures a Restricted provider.
type RestrictedOption func(*Restricted)

// WithDescriptions sets description overrides keyed by exposed operation name.
func WithDescriptions(descs map[string]string) RestrictedOption {
	return func(r *Restricted) { r.descriptions = descs }
}

// Compile-time interface checks.
var (
	_ core.Provider        = (*Restricted)(nil)
	_ core.ManualProvider  = (*Restricted)(nil)
	_ core.CatalogProvider = (*Restricted)(nil)
	_ core.AuthTypeLister  = (*Restricted)(nil)
	_ core.OAuthProvider   = (*restrictedOAuth)(nil)
	_ core.ManualProvider  = (*restrictedOAuth)(nil)
)

// NewRestricted returns a Provider that gates operations to the allowed set.
// The ops map maps exposedName -> innerName. If innerName is empty, the
// exposed name equals the inner name (no alias). If the inner provider
// implements OAuthProvider, the returned value does too.
func NewRestricted(inner core.Provider, ops map[string]string, opts ...RestrictedOption) core.Provider {
	m := make(map[string]struct{}, len(ops))
	aliases := make(map[string]string)
	reverseAlias := make(map[string]string)
	allowedInner := make(map[string]struct{}, len(ops))
	for exposed, innerName := range ops {
		m[exposed] = struct{}{}
		if innerName != "" && innerName != exposed {
			aliases[exposed] = innerName
			reverseAlias[innerName] = exposed
			allowedInner[innerName] = struct{}{}
		} else {
			allowedInner[exposed] = struct{}{}
		}
	}
	r := &Restricted{
		inner:        inner,
		allowed:      m,
		aliases:      aliases,
		reverseAlias: reverseAlias,
		allowedInner: allowedInner,
	}
	for _, opt := range opts {
		opt(r)
	}
	if scp, ok := inner.(core.SessionCatalogProvider); ok {
		rs := &restrictedSession{Restricted: r, scp: scp}
		if oauth, ok := inner.(core.OAuthProvider); ok {
			return &restrictedOAuth{Restricted: rs.Restricted, oauth: oauth, session: rs}
		}
		return rs
	}
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
	return OperationsList(r.Catalog())
}

func (r *Restricted) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if _, ok := r.allowed[operation]; !ok {
		return nil, fmt.Errorf("operation %q is not allowed", operation)
	}
	innerName := operation
	if alias, ok := r.aliases[operation]; ok {
		innerName = alias
	}
	return r.inner.Execute(ctx, innerName, params, token)
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
	return r.filterCatalog(cat)
}

func (r *Restricted) filterCatalog(cat *catalog.Catalog) *catalog.Catalog {
	filtered := *cat
	filtered.Operations = nil
	for i := range cat.Operations {
		if _, ok := r.allowedInner[cat.Operations[i].ID]; ok {
			op := cat.Operations[i]
			if exposed, ok := r.reverseAlias[op.ID]; ok {
				op.ID = exposed
			}
			if desc, ok := r.descriptions[op.ID]; ok {
				op.Description = desc
			}
			filtered.Operations = append(filtered.Operations, op)
		}
	}
	return &filtered
}

func (r *Restricted) Close() error {
	if c, ok := r.inner.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// Inner returns the unwrapped provider.
func (r *Restricted) Inner() core.Provider {
	return r.inner
}

// restrictedSession wraps a Restricted provider and adds SessionCatalogProvider
// support. Only returned when the inner provider actually implements it.
type restrictedSession struct {
	*Restricted
	scp core.SessionCatalogProvider
}

func (rs *restrictedSession) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	cat, err := rs.scp.CatalogForRequest(ctx, token)
	if err != nil || cat == nil {
		return cat, err
	}
	return rs.filterCatalog(cat), nil
}

// restrictedOAuth wraps a Restricted provider and delegates OAuth methods
// to the inner OAuthProvider.
type restrictedOAuth struct {
	*Restricted
	oauth   core.OAuthProvider
	session *restrictedSession
}

func (r *restrictedOAuth) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if r.session != nil {
		return r.session.CatalogForRequest(ctx, token)
	}
	return nil, nil
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

type restrictedOAuthVerifierExchanger interface {
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
}

func (r *restrictedOAuth) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	if exchanger, ok := r.oauth.(restrictedOAuthVerifierExchanger); ok {
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

func (r *restrictedOAuth) StartOAuth(state string, scopes []string) (string, string) {
	type starter interface {
		StartOAuth(state string, scopes []string) (string, string)
	}
	if s, ok := r.oauth.(starter); ok {
		return s.StartOAuth(state, scopes)
	}
	return r.oauth.AuthorizationURL(state, scopes), ""
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

func (r *Restricted) AuthTypes() []string {
	if atl, ok := r.inner.(core.AuthTypeLister); ok {
		return atl.AuthTypes()
	}
	return nil
}

func (r *Restricted) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	if cpp, ok := r.inner.(core.ConnectionParamProvider); ok {
		return cpp.ConnectionParamDefs()
	}
	return nil
}

func (r *Restricted) CredentialFields() []core.CredentialFieldDef {
	if cfp, ok := r.inner.(core.CredentialFieldsProvider); ok {
		return cfp.CredentialFields()
	}
	return nil
}
