package integration

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/apiexec"
	"github.com/valon-technologies/gestalt/internal/oauth"
)

var (
	_ core.OAuthProvider   = (*Base)(nil)
	_ core.ManualProvider  = (*Base)(nil)
	_ core.CatalogProvider = (*Base)(nil)
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
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*core.TokenResponse, error)
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

func (u UpstreamAuth) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*core.TokenResponse, error) {
	var opts []oauth.ExchangeOption
	if verifier != "" {
		opts = append(opts, oauth.WithPKCEVerifier(verifier))
	}
	return u.Handler.ExchangeCode(ctx, code, opts...)
}

func (u UpstreamAuth) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return u.Handler.RefreshToken(ctx, refreshToken)
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

	TokenParser    func(token string) (authHeader string, extraHeaders map[string]string, err error)
	CheckResponse  apiexec.ResponseChecker
	RequestMutator func(operation string, req *apiexec.Request, params map[string]any) error
	ExecuteFunc    func(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error)

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

func (b *Base) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*core.TokenResponse, error) {
	if exchanger, ok := b.Auth.(oauthVerifierExchanger); ok {
		return exchanger.ExchangeCodeWithVerifier(ctx, code, verifier)
	}
	return b.Auth.ExchangeCode(ctx, code)
}

func (b *Base) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return b.Auth.RefreshToken(ctx, refreshToken)
}

func (b *Base) ListOperations() []core.Operation { return b.Operations }

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

	req := apiexec.Request{
		Method:        ep.Method,
		BaseURL:       b.BaseURL,
		Path:          ep.Path,
		Params:        params,
		CustomHeaders: copyHeaders(b.Headers),
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

	return apiexec.Do(ctx, b.httpClient(), req)
}

func (b *Base) executeGraphQL(ctx context.Context, query string, params map[string]any, token string) (*core.OperationResult, error) {
	gqlReq := apiexec.GraphQLRequest{
		URL:           b.BaseURL,
		Query:         query,
		Variables:     params,
		CustomHeaders: copyHeaders(b.Headers),
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
