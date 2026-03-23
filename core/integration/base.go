package integration

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/apiexec"
	"github.com/valon-technologies/gestalt/internal/oauth"
	"github.com/valon-technologies/gestalt/internal/paraminterp"
)

var (
	_ core.OAuthProvider           = (*Base)(nil)
	_ core.ManualProvider          = (*Base)(nil)
	_ core.CatalogProvider         = (*Base)(nil)
	_ core.ConnectionParamProvider = (*Base)(nil)
	_ core.PostConnectProvider     = (*Base)(nil)
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

	TokenParser    func(token string) (authHeader string, extraHeaders map[string]string, err error)
	CheckResponse  apiexec.ResponseChecker
	RequestMutator func(operation string, req *apiexec.Request, params map[string]any) error
	ExecuteFunc    func(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error)

	ConnectionDefs    map[string]core.ConnectionParamDef
	PostConnectHookFn core.PostConnectHook

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
	mc, ok := b.Auth.(manualChecker)
	return ok && mc.IsManual()
}

func (b *Base) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return b.ConnectionDefs
}

func (b *Base) PostConnectHook() core.PostConnectHook {
	return b.PostConnectHookFn
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
	headers := copyHeaders(b.Headers)
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
		return b.executeGraphQL(ctx, query, params, token)
	}

	ep, ok := b.Endpoints[operation]
	if !ok {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	baseURL, headers := b.resolvedURLAndHeaders(ctx)

	req := apiexec.Request{
		Method:        ep.Method,
		BaseURL:       baseURL,
		Path:          ep.Path,
		Params:        params,
		CustomHeaders: headers,
		CheckResponse: b.CheckResponse,
	}

	if err := b.applyAuth(&req, token); err != nil {
		return nil, err
	}

	if b.RequestMutator != nil {
		if err := b.RequestMutator(operation, &req, params); err != nil {
			return nil, err
		}
	}

	if pgn, ok := b.Pagination[operation]; ok {
		return apiexec.DoPaginated(ctx, b.httpClient(), req, pgn)
	}

	return apiexec.Do(ctx, b.httpClient(), req)
}

func (b *Base) executeGraphQL(ctx context.Context, query string, params map[string]any, token string) (*core.OperationResult, error) {
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
	return apiexec.DoGraphQL(ctx, b.httpClient(), gqlReq)
}

type resolvedAuth struct {
	token        string
	authHeader   string
	extraHeaders map[string]string
}

func (b *Base) resolveAuth(token string) (resolvedAuth, error) {
	if b.TokenParser != nil {
		authHeader, extraHeaders, err := b.TokenParser(token)
		if err != nil {
			return resolvedAuth{}, err
		}
		return resolvedAuth{authHeader: authHeader, extraHeaders: extraHeaders}, nil
	}
	switch b.AuthStyle {
	case AuthStyleBearer:
		return resolvedAuth{token: token}, nil
	case AuthStyleRaw:
		return resolvedAuth{authHeader: token}, nil
	case AuthStyleBasic:
		return resolvedAuth{authHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte(token))}, nil
	default:
		return resolvedAuth{}, nil
	}
}

func (b *Base) applyAuth(req *apiexec.Request, token string) error {
	auth, err := b.resolveAuth(token)
	if err != nil {
		return err
	}
	req.Token = auth.token
	req.AuthHeader = auth.authHeader
	for k, v := range auth.extraHeaders {
		if req.CustomHeaders == nil {
			req.CustomHeaders = make(map[string]string)
		}
		req.CustomHeaders[k] = v
	}
	return nil
}

func (b *Base) applyGraphQLAuth(req *apiexec.GraphQLRequest, token string) error {
	auth, err := b.resolveAuth(token)
	if err != nil {
		return err
	}
	req.Token = auth.token
	req.AuthHeader = auth.authHeader
	for k, v := range auth.extraHeaders {
		if req.CustomHeaders == nil {
			req.CustomHeaders = make(map[string]string)
		}
		req.CustomHeaders[k] = v
	}
	return nil
}

func copyHeaders(h map[string]string) map[string]string {
	if h == nil {
		return nil
	}
	c := make(map[string]string, len(h))
	for k, v := range h {
		c[k] = v
	}
	return c
}

func mergeHeaders(baseHeaders, overrideHeaders map[string]string) map[string]string {
	if len(baseHeaders) == 0 && len(overrideHeaders) == 0 {
		return nil
	}

	merged := copyHeaders(baseHeaders)
	if merged == nil {
		merged = make(map[string]string, len(overrideHeaders))
	}
	for key, value := range overrideHeaders {
		merged[key] = value
	}
	return merged
}
