package integration

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
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
	allowedRoles map[string][]string
	tags         map[string][]string
}

// RestrictedOption configures a Restricted provider.
type RestrictedOption func(*Restricted)

// WithDescriptions sets description overrides keyed by exposed operation name.
func WithDescriptions(descs map[string]string) RestrictedOption {
	return func(r *Restricted) { r.descriptions = descs }
}

// WithAllowedRoles sets allowedRoles overrides keyed by exposed operation name.
func WithAllowedRoles(roles map[string][]string) RestrictedOption {
	return func(r *Restricted) { r.allowedRoles = roles }
}

// WithTags adds provider-owned search tags keyed by exposed operation name.
func WithTags(tags map[string][]string) RestrictedOption {
	return func(r *Restricted) { r.tags = tags }
}

// Compile-time interface checks.
var (
	_ core.Provider      = (*Restricted)(nil)
	_ core.OAuthProvider = (*restrictedOAuth)(nil)
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
			return &restrictedOAuth{Restricted: rs.Restricted, inner: oauth, session: rs}
		}
		return rs
	}
	if oauth, ok := inner.(core.OAuthProvider); ok {
		return &restrictedOAuth{Restricted: r, inner: oauth}
	}
	return r
}

func (r *Restricted) Name() string                        { return r.inner.Name() }
func (r *Restricted) DisplayName() string                 { return r.inner.DisplayName() }
func (r *Restricted) Description() string                 { return r.inner.Description() }
func (r *Restricted) ConnectionMode() core.ConnectionMode { return r.inner.ConnectionMode() }

func (r *Restricted) SetDisplayName(s string) {
	if v, ok := r.inner.(interface{ SetDisplayName(string) }); ok {
		v.SetDisplayName(s)
	}
}

func (r *Restricted) SetDescription(s string) {
	if v, ok := r.inner.(interface{ SetDescription(string) }); ok {
		v.SetDescription(s)
	}
}

func (r *Restricted) SetIconSVG(s string) {
	if v, ok := r.inner.(interface{ SetIconSVG(string) }); ok {
		v.SetIconSVG(s)
	}
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

func (r *Restricted) InvokeGraphQL(ctx context.Context, request core.GraphQLRequest, token string) (*core.OperationResult, error) {
	invoker, ok := r.inner.(core.GraphQLSurfaceInvoker)
	if !ok {
		return nil, fmt.Errorf("graphql surface is not available")
	}
	return invoker.InvokeGraphQL(ctx, request, token)
}

func (r *Restricted) Catalog() *catalog.Catalog {
	cat := r.inner.Catalog()
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
			if roles, ok := r.allowedRoles[op.ID]; ok {
				op.AllowedRoles = append([]string(nil), roles...)
			}
			if tags, ok := r.tags[op.ID]; ok {
				op.Tags = catalog.MergeTags(op.Tags, tags)
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
	inner   core.OAuthProvider
	session *restrictedSession
}

func (r *restrictedOAuth) AuthorizationURL(state string, scopes []string) string {
	return r.inner.AuthorizationURL(state, scopes)
}

func (r *restrictedOAuth) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return r.inner.ExchangeCode(ctx, code)
}

func (r *restrictedOAuth) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return r.inner.RefreshToken(ctx, refreshToken)
}

func (r *restrictedOAuth) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if r.session != nil {
		return r.session.CatalogForRequest(ctx, token)
	}
	return nil, nil
}

func (r *Restricted) AuthTypes() []string {
	return r.inner.AuthTypes()
}

func (r *Restricted) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return r.inner.ConnectionParamDefs()
}

func (r *Restricted) CredentialFields() []core.CredentialFieldDef {
	return r.inner.CredentialFields()
}

func (r *Restricted) DiscoveryConfig() *core.DiscoveryConfig {
	return r.inner.DiscoveryConfig()
}

func (r *Restricted) SupportsPostConnect() bool {
	return core.SupportsPostConnect(r.inner)
}

func (r *Restricted) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if pcp, ok := r.inner.(core.PostConnectCapable); ok {
		return pcp.PostConnect(ctx, token)
	}
	return nil, core.ErrPostConnectUnsupported
}

func (r *Restricted) SupportsHTTPSubject() bool {
	return core.SupportsHTTPSubject(r.inner)
}

func (r *Restricted) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	subject, _, err := core.ResolveHTTPSubject(ctx, r.inner, req)
	return subject, err
}

func (r *Restricted) ConnectionForOperation(operation string) string {
	if _, ok := r.allowed[operation]; !ok {
		return ""
	}
	innerOperation := operation
	if alias, ok := r.aliases[operation]; ok {
		innerOperation = alias
	}
	return r.inner.ConnectionForOperation(innerOperation)
}

func (r *Restricted) ResolveConnectionForOperation(operation string, params map[string]any) (string, error) {
	if _, ok := r.allowed[operation]; !ok {
		return "", nil
	}
	innerOperation := operation
	if alias, ok := r.aliases[operation]; ok {
		innerOperation = alias
	}
	if resolver, ok := r.inner.(core.OperationConnectionResolver); ok {
		return resolver.ResolveConnectionForOperation(innerOperation, params)
	}
	return r.inner.ConnectionForOperation(innerOperation), nil
}

func (r *Restricted) OperationConnectionOverrideAllowed(operation string, params map[string]any) bool {
	if _, ok := r.allowed[operation]; !ok {
		return true
	}
	innerOperation := operation
	if alias, ok := r.aliases[operation]; ok {
		innerOperation = alias
	}
	if policy, ok := r.inner.(core.OperationConnectionOverridePolicy); ok {
		return policy.OperationConnectionOverrideAllowed(innerOperation, params)
	}
	return false
}
