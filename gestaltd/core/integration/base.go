package integration

import (
	"context"
	"fmt"
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
	_ core.ConnectionParamProvider = (*Base)(nil)
	_ core.DiscoveryConfigProvider = (*Base)(nil)
)

type manualChecker interface{ IsManual() bool }

type AuthHandler interface {
	AuthorizationURL(state string, scopes []string) string
	ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error)
}

// UpstreamAuth trims oauth.UpstreamHandler down to the provider auth surface.
type UpstreamAuth struct {
	*oauth.UpstreamHandler
}

func (u UpstreamAuth) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return u.UpstreamHandler.ExchangeCode(ctx, code)
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

func (b *Base) SetCatalog(c *catalog.Catalog) {
	if c != nil {
		c.SortOperations()
	}
	b.catalog = c
}

func (b *Base) Catalog() *catalog.Catalog { return b.catalog }

func (b *Base) AuthorizationURL(state string, scopes []string) string {
	return b.Auth.AuthorizationURL(state, scopes)
}

func (b *Base) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return b.Auth.ExchangeCode(ctx, code)
}

func (b *Base) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return b.Auth.RefreshToken(ctx, refreshToken)
}

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

	catOp := findCatalogOp(b.catalog, operation)
	if catOp == nil {
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}
	if catOp.Query != "" {
		return b.executeGraphQL(ctx, operation, catOp.Query, params, token)
	}
	return b.executeREST(ctx, operation, catOp, params, token)
}

func (b *Base) executeGraphQL(ctx context.Context, operation string, query string, params map[string]any, token string) (*core.OperationResult, error) {
	gqlURL, headers := b.resolvedURLAndHeaders(ctx)
	credential, err := b.materializeCredential(token)
	if err != nil {
		return nil, err
	}
	headers = egress.ApplyHeaderMutations(headers, credential.Headers)
	authCredential := egress.CredentialMaterialization{Authorization: credential.Authorization}

	gqlReq := apiexec.GraphQLRequest{
		URL:           gqlURL,
		Query:         query,
		Variables:     params,
		AuthHeader:    authCredential.Authorization,
		CustomHeaders: headers,
	}

	resolved, err := b.resolveGraphQLEgress(ctx, operation, gqlReq, authCredential)
	if err != nil {
		return nil, err
	}
	gqlReq.AuthHeader = resolved.Credential.Authorization
	gqlReq.CustomHeaders = resolved.Headers

	return apiexec.DoGraphQL(ctx, b.httpClient(), gqlReq)
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
