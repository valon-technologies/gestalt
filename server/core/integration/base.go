package integration

import (
	"context"
	"maps"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/apiexec"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/oauth"
	"github.com/valon-technologies/gestalt/server/internal/paraminterp"
)

var (
	_ core.OAuthProvider           = (*Base)(nil)
	_ core.ManualProvider          = (*Base)(nil)
	_ core.CatalogProvider         = (*Base)(nil)
	_ core.ConnectionParamProvider = (*Base)(nil)
	_ core.DiscoveryConfigProvider = (*Base)(nil)
)

type manualChecker interface{ IsManual() bool }

type AuthHandler interface {
	AuthorizationURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
}

// UpstreamAuth adapts *oauth.UpstreamHandler to the AuthHandler interface.
type UpstreamAuth struct {
	Handler *oauth.UpstreamHandler
}

type oauthStarter interface {
	StartOAuth(state string, scopes []string) (authURL string, verifier string)
}

type oauthVerifierExchanger interface {
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error)
}

func (u UpstreamAuth) AuthorizationURL(state string, scopes []string) string {
	return u.Handler.AuthorizationURL(state, scopes)
}

func (u UpstreamAuth) StartOAuth(state string, scopes []string) (string, string) {
	return u.Handler.AuthorizationURLWithPKCE(state, scopes)
}

func (u UpstreamAuth) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return u.Handler.ExchangeCode(ctx, code)
}

func (u UpstreamAuth) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	opts = append(opts, extraOpts...)
	return u.Handler.ExchangeCode(ctx, code, opts...)
}

func (u UpstreamAuth) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return u.Handler.RefreshToken(ctx, refreshToken)
}

func (u UpstreamAuth) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	return u.Handler.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
}

func (u UpstreamAuth) TokenURL() string             { return u.Handler.TokenURL() }
func (u UpstreamAuth) AuthorizationBaseURL() string { return u.Handler.AuthorizationBaseURL() }

func (u UpstreamAuth) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	return u.Handler.AuthorizationURLWithOverride(authBaseURL, state, scopes)
}

type Endpoint struct {
	Method string
	Path   string
}

type AuthStyle int

const (
	AuthStyleBearer AuthStyle = iota
	AuthStyleRaw
	AuthStyleNone
	AuthStyleBasic
)

type Base struct {
	IntegrationName    string
	IntegrationDisplay string
	IntegrationDesc    string
	ConnMode           core.ConnectionMode
	Auth               AuthHandler
	BaseURL            string
	Operations         []core.Operation
	Endpoints          map[string]Endpoint
	Queries            map[string]string // operation_name -> graphql query
	Headers            map[string]string
	AuthStyle          AuthStyle
	HTTPClient         *http.Client
	Pagination         map[string]apiexec.PaginationConfig

	TokenParser     func(token string) (authHeader string, extraHeaders map[string]string, err error)
	CheckResponse   apiexec.ResponseChecker
	ExecuteFunc     func(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error)
	EgressResolver  *egress.Resolver
	ResponseMapping *ResponseMappingConfig

	ConnectionDefs      map[string]core.ConnectionParamDef
	DiscoveryDef        *core.DiscoveryConfig
	ManualAuthEnabled   bool
	CredentialFieldDefs []core.CredentialFieldDef

	catalog *catalog.Catalog
}

func (b *Base) Name() string        { return b.IntegrationName }
func (b *Base) DisplayName() string { return b.IntegrationDisplay }
func (b *Base) Description() string { return b.IntegrationDesc }

func (b *Base) ConnectionMode() core.ConnectionMode {
	if b.ConnMode == "" {
		return core.ConnectionModeUser
	}
	return b.ConnMode
}

func (b *Base) SupportsManualAuth() bool {
	if b.ManualAuthEnabled {
		return true
	}
	mc, ok := b.Auth.(manualChecker)
	return ok && mc.IsManual()
}

func (b *Base) AuthTypes() []string {
	mc, ok := b.Auth.(manualChecker)
	manualOnly := ok && mc.IsManual()
	if manualOnly {
		return []string{"manual"}
	}
	if b.ManualAuthEnabled {
		return []string{"oauth", "manual"}
	}
	return []string{"oauth"}
}

func (b *Base) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return b.ConnectionDefs
}

func (b *Base) CredentialFields() []core.CredentialFieldDef {
	return b.CredentialFieldDefs
}

func (b *Base) DiscoveryConfig() *core.DiscoveryConfig {
	return b.DiscoveryDef
}

func (b *Base) TokenURL() string {
	type tokenURLer interface{ TokenURL() string }
	if tu, ok := b.Auth.(tokenURLer); ok {
		return tu.TokenURL()
	}
	return ""
}

func (b *Base) AuthorizationBaseURL() string {
	type authBaseURLer interface{ AuthorizationBaseURL() string }
	if abu, ok := b.Auth.(authBaseURLer); ok {
		return abu.AuthorizationBaseURL()
	}
	return ""
}

func (b *Base) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	type overrider interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}
	if ov, ok := b.Auth.(overrider); ok {
		return ov.StartOAuthWithOverride(authBaseURL, state, scopes)
	}
	return b.Auth.AuthorizationURL(state, scopes), ""
}

func (b *Base) SetCatalog(c *catalog.Catalog) { b.catalog = c }

func (b *Base) Catalog() *catalog.Catalog { return b.catalog }

func (b *Base) AuthorizationURL(state string, scopes []string) string {
	return b.Auth.AuthorizationURL(state, scopes)
}

func (b *Base) StartOAuth(state string, scopes []string) (string, string) {
	if starter, ok := b.Auth.(oauthStarter); ok {
		return starter.StartOAuth(state, scopes)
	}
	return b.Auth.AuthorizationURL(state, scopes), ""
}

func (b *Base) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return b.Auth.ExchangeCode(ctx, code)
}

func (b *Base) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...oauth.ExchangeOption) (*core.TokenResponse, error) {
	if exchanger, ok := b.Auth.(oauthVerifierExchanger); ok {
		return exchanger.ExchangeCodeWithVerifier(ctx, code, verifier, extraOpts...)
	}
	return b.Auth.ExchangeCode(ctx, code)
}

func (b *Base) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return b.Auth.RefreshToken(ctx, refreshToken)
}

func (b *Base) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	type refresher interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}
	if r, ok := b.Auth.(refresher); ok {
		return r.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
	}
	return b.Auth.RefreshToken(ctx, refreshToken)
}

func (b *Base) ListOperations() []core.Operation { return b.Operations }

func (b *Base) resolvedURLAndHeaders(ctx context.Context) (string, map[string]string) {
	baseURL := b.BaseURL
	headers := maps.Clone(b.Headers)
	if cp := core.ConnectionParams(ctx); cp != nil {
		baseURL = paraminterp.Interpolate(baseURL, cp)
		for k, v := range headers {
			headers[k] = paraminterp.Interpolate(v, cp)
		}
	}
	return baseURL, headers
}

func (b *Base) httpClient() *http.Client {
	if b.HTTPClient != nil {
		return b.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (b *Base) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if b.ExecuteFunc != nil {
		return b.ExecuteFunc(ctx, operation, params, token)
	}

	if query, ok := b.Queries[operation]; ok {
		return b.executeGraphQL(ctx, operation, query, params, token)
	}

	return b.executeREST(ctx, operation, params, token)
}

func (b *Base) executeGraphQL(ctx context.Context, operation string, query string, params map[string]any, token string) (*core.OperationResult, error) {
	gqlURL, headers := b.resolvedURLAndHeaders(ctx)

	gqlReq := apiexec.GraphQLRequest{
		URL:           gqlURL,
		Query:         query,
		Variables:     params,
		CustomHeaders: headers,
	}
	if err := b.applyGraphQLAuth(&gqlReq, token); err != nil {
		return nil, err
	}

	resolved, err := b.resolveGraphQLEgress(ctx, operation, gqlReq)
	if err != nil {
		return nil, err
	}

	return egress.ExecuteGraphQL(ctx, b.httpClient(), egress.GraphQLRequestSpec{
		Target:     resolved.Target,
		URL:        gqlReq.URL,
		Query:      gqlReq.Query,
		Variables:  gqlReq.Variables,
		Headers:    resolved.Headers,
		Credential: resolved.Credential,
	})
}

func (b *Base) egressAuthStyle() egress.AuthStyle {
	switch b.AuthStyle {
	case AuthStyleRaw:
		return egress.AuthStyleRaw
	case AuthStyleNone:
		return egress.AuthStyleNone
	case AuthStyleBasic:
		return egress.AuthStyleBasic
	default:
		return egress.AuthStyleBearer
	}
}

func (b *Base) materializeCredential(token string) (egress.CredentialMaterialization, error) {
	return egress.MaterializeCredential(token, b.egressAuthStyle(), b.TokenParser)
}

func (b *Base) applyAuth(req *apiexec.Request, token string) error {
	auth, err := b.materializeCredential(token)
	if err != nil {
		return err
	}
	req.Token, req.AuthHeader = b.requestAuthFields(token, auth)
	for _, header := range auth.Headers {
		if req.CustomHeaders == nil {
			req.CustomHeaders = make(map[string]string)
		}
		req.CustomHeaders[header.Name] = header.Value
	}
	return nil
}

func (b *Base) applyGraphQLAuth(req *apiexec.GraphQLRequest, token string) error {
	auth, err := b.materializeCredential(token)
	if err != nil {
		return err
	}
	req.Token, req.AuthHeader = b.requestAuthFields(token, auth)
	for _, header := range auth.Headers {
		if req.CustomHeaders == nil {
			req.CustomHeaders = make(map[string]string)
		}
		req.CustomHeaders[header.Name] = header.Value
	}
	return nil
}

func (b *Base) requestAuthFields(token string, auth egress.CredentialMaterialization) (string, string) {
	if b.TokenParser == nil && b.AuthStyle == AuthStyleBearer {
		return token, ""
	}
	return "", auth.Authorization
}
