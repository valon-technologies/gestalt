package bigquery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/bigquery"
	"golang.org/x/oauth2"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	inoauth "github.com/valon-technologies/gestalt/internal/oauth"
)

const (
	operationQuery = "query"

	paramProjectID    = "project_id"
	paramQuery        = "query"
	paramMaxResults   = "max_results"
	paramTimeoutMs    = "timeout_ms"
	paramUseLegacySQL = "use_legacy_sql"

	defaultMaxResults  = 500
	maxMaxResults      = 10000
	defaultTimeoutMs   = 60000
	defaultQueryMethod = "POST"

	fieldModeRepeated = "REPEATED"
	fieldModeRequired = "REQUIRED"
	fieldModeNullable = "NULLABLE"
)

// NewFactory returns a ProviderFactory for BigQuery that wraps the default
// factory and adds a custom "query" operation backed by the BigQuery Go SDK.
func NewFactory(defaultFactory bootstrap.ProviderFactory) bootstrap.ProviderFactory {
	return func(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (core.Provider, error) {
		restProv, err := defaultFactory(ctx, name, intg, deps)
		if err != nil {
			return nil, err
		}
		return wrapProvider(restProv, queryAllowed(intg)), nil
	}
}

// queryAllowed checks whether the custom query operation should be exposed.
// If any upstream sets allowed_operations, query must be explicitly listed.
func queryAllowed(intg config.IntegrationDef) bool {
	for i := range intg.Upstreams {
		if intg.Upstreams[i].AllowedOperations != nil {
			if _, ok := intg.Upstreams[i].AllowedOperations[operationQuery]; ok {
				return true
			}
			return false
		}
	}
	return true
}

func wrapProvider(inner core.Provider, addQuery bool) core.Provider {
	p := &provider{inner: inner, addQuery: addQuery}
	if oauth, ok := inner.(core.OAuthProvider); ok {
		return &oauthProvider{provider: p, oauth: oauth}
	}
	return p
}

type provider struct {
	inner    core.Provider
	addQuery bool
}

var _ core.Provider = (*provider)(nil)

func (p *provider) Name() string                        { return p.inner.Name() }
func (p *provider) DisplayName() string                 { return p.inner.DisplayName() }
func (p *provider) Description() string                 { return p.inner.Description() }
func (p *provider) ConnectionMode() core.ConnectionMode { return p.inner.ConnectionMode() }

func (p *provider) ListOperations() []core.Operation {
	inner := p.inner.ListOperations()
	if !p.addQuery {
		return inner
	}
	ops := make([]core.Operation, len(inner), len(inner)+1)
	copy(ops, inner)
	return append(ops, queryOperation())
}

func (p *provider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if p.addQuery && operation == operationQuery {
		return executeQuery(ctx, params, token)
	}
	return p.inner.Execute(ctx, operation, params, token)
}

func (p *provider) Catalog() *catalog.Catalog {
	cp, ok := p.inner.(core.CatalogProvider)
	if !ok {
		return nil
	}
	cat := cp.Catalog()
	if cat == nil {
		return nil
	}
	if !p.addQuery {
		return cat
	}
	out := cat.Clone()
	out.Operations = append(out.Operations, queryCatalogOperation())
	return out
}

func (p *provider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if scp, ok := p.inner.(core.SessionCatalogProvider); ok {
		return scp.CatalogForRequest(ctx, token)
	}
	return nil, nil
}

func (p *provider) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	type caller interface {
		CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
	}
	if c, ok := p.inner.(caller); ok {
		return c.CallTool(ctx, name, args)
	}
	return nil, fmt.Errorf("inner provider does not support CallTool")
}

func (p *provider) Inner() core.Provider { return p.inner }

func (p *provider) SupportsManualAuth() bool {
	if mp, ok := p.inner.(core.ManualProvider); ok {
		return mp.SupportsManualAuth()
	}
	return false
}

func (p *provider) AuthTypes() []string {
	if atl, ok := p.inner.(core.AuthTypeLister); ok {
		return atl.AuthTypes()
	}
	return nil
}

func (p *provider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	if cpp, ok := p.inner.(core.ConnectionParamProvider); ok {
		return cpp.ConnectionParamDefs()
	}
	return nil
}

func (p *provider) PostConnectHook() core.PostConnectHook {
	if pcp, ok := p.inner.(core.PostConnectProvider); ok {
		return pcp.PostConnectHook()
	}
	return nil
}

type oauthProvider struct {
	*provider
	oauth core.OAuthProvider
}

func (o *oauthProvider) AuthorizationURL(state string, scopes []string) string {
	return o.oauth.AuthorizationURL(state, scopes)
}

func (o *oauthProvider) ExchangeCode(ctx context.Context, code string) (*core.TokenResponse, error) {
	return o.oauth.ExchangeCode(ctx, code)
}

func (o *oauthProvider) RefreshToken(ctx context.Context, refreshToken string) (*core.TokenResponse, error) {
	return o.oauth.RefreshToken(ctx, refreshToken)
}

func (o *oauthProvider) RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error) {
	type refresher interface {
		RefreshTokenWithURL(ctx context.Context, refreshToken, tokenURL string) (*core.TokenResponse, error)
	}
	if rw, ok := o.oauth.(refresher); ok {
		return rw.RefreshTokenWithURL(ctx, refreshToken, tokenURL)
	}
	return o.oauth.RefreshToken(ctx, refreshToken)
}

func (o *oauthProvider) StartOAuth(state string, scopes []string) (string, string) {
	type starter interface {
		StartOAuth(state string, scopes []string) (string, string)
	}
	if s, ok := o.oauth.(starter); ok {
		return s.StartOAuth(state, scopes)
	}
	return o.oauth.AuthorizationURL(state, scopes), ""
}

func (o *oauthProvider) StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string) {
	type overrider interface {
		StartOAuthWithOverride(authBaseURL, state string, scopes []string) (string, string)
	}
	if ov, ok := o.oauth.(overrider); ok {
		return ov.StartOAuthWithOverride(authBaseURL, state, scopes)
	}
	return o.oauth.AuthorizationURL(state, scopes), ""
}

func (o *oauthProvider) AuthorizationBaseURL() string {
	type authBaseURLer interface{ AuthorizationBaseURL() string }
	if abu, ok := o.oauth.(authBaseURLer); ok {
		return abu.AuthorizationBaseURL()
	}
	return ""
}

func (o *oauthProvider) TokenURL() string {
	type tokenURLer interface{ TokenURL() string }
	if tu, ok := o.oauth.(tokenURLer); ok {
		return tu.TokenURL()
	}
	return ""
}

func (o *oauthProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...inoauth.ExchangeOption) (*core.TokenResponse, error) {
	type exchanger interface {
		ExchangeCodeWithVerifier(ctx context.Context, code, verifier string, extraOpts ...inoauth.ExchangeOption) (*core.TokenResponse, error)
	}
	if e, ok := o.oauth.(exchanger); ok {
		return e.ExchangeCodeWithVerifier(ctx, code, verifier, extraOpts...)
	}
	return o.oauth.ExchangeCode(ctx, code)
}

var queryParams = []core.Parameter{
	{Name: paramProjectID, Type: "string", Description: "GCP project ID", Required: true},
	{Name: paramQuery, Type: "string", Description: "SQL query string", Required: true},
	{Name: paramMaxResults, Type: "integer", Description: "Maximum rows to return (default 500, max 10000)", Default: defaultMaxResults},
	{Name: paramTimeoutMs, Type: "integer", Description: "Query timeout in milliseconds (default 60000)", Default: defaultTimeoutMs},
	{Name: paramUseLegacySQL, Type: "boolean", Description: "Use legacy SQL dialect (default false)", Default: false},
}

func queryOperation() core.Operation {
	return core.Operation{
		Name:        operationQuery,
		Description: "Execute a BigQuery SQL query and return results synchronously",
		Method:      defaultQueryMethod,
		Parameters:  queryParams,
	}
}

func queryCatalogOperation() catalog.CatalogOperation {
	readOnly := false
	catParams := make([]catalog.CatalogParameter, len(queryParams))
	for i, p := range queryParams {
		catParams[i] = catalog.CatalogParameter{
			Name:        p.Name,
			Type:        p.Type,
			Description: p.Description,
			Required:    p.Required,
			Default:     p.Default,
		}
	}
	return catalog.CatalogOperation{
		ID:          operationQuery,
		Method:      defaultQueryMethod,
		Path:        "/query",
		Title:       "Execute SQL Query",
		Description: "Execute a BigQuery SQL query and return results synchronously",
		Transport:   catalog.TransportHTTP,
		Annotations: catalog.OperationAnnotations{
			ReadOnlyHint: &readOnly,
		},
		Parameters: catParams,
	}
}

type queryResult struct {
	Schema    []schemaField    `json:"schema"`
	Rows      []map[string]any `json:"rows"`
	TotalRows uint64           `json:"total_rows"`
	Complete  bool             `json:"job_complete"`
}

type schemaField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

func executeQuery(ctx context.Context, params map[string]any, token string) (*core.OperationResult, error) {
	projectID, _ := params[paramProjectID].(string)
	if projectID == "" {
		return nil, fmt.Errorf("%s is required", paramProjectID)
	}
	sql, _ := params[paramQuery].(string)
	if sql == "" {
		return nil, fmt.Errorf("%s is required", paramQuery)
	}

	maxResults := intParam(params, paramMaxResults, defaultMaxResults)
	if maxResults > maxMaxResults {
		maxResults = maxMaxResults
	}
	timeoutMs := intParam(params, paramTimeoutMs, defaultTimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = defaultTimeoutMs
	}
	useLegacySQL, _ := params[paramUseLegacySQL].(bool)

	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client, err := bigquery.NewClient(ctx, projectID, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("creating bigquery client: %w", err)
	}
	defer func() { _ = client.Close() }()

	q := client.Query(sql)
	q.UseLegacySQL = useLegacySQL

	it, err := q.Read(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("query timed out after %dms; try simplifying the query or increasing timeout_ms", timeoutMs)
		}
		return nil, fmt.Errorf("executing query: %w", err)
	}

	schema := make([]schemaField, 0, len(it.Schema))
	for _, f := range it.Schema {
		schema = append(schema, schemaField{
			Name: f.Name,
			Type: string(f.Type),
			Mode: fieldMode(f),
		})
	}

	rows := make([]map[string]any, 0, min(maxResults, 100))
	for i := 0; i < maxResults; i++ {
		var row map[string]bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}
		rows = append(rows, valuesToAny(row))
	}

	result := queryResult{
		Schema:    schema,
		Rows:      rows,
		TotalRows: it.TotalRows,
		Complete:  true,
	}

	body, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}

	return &core.OperationResult{
		Status: http.StatusOK,
		Body:   string(body),
	}, nil
}

func fieldMode(f *bigquery.FieldSchema) string {
	if f.Repeated {
		return fieldModeRepeated
	}
	if f.Required {
		return fieldModeRequired
	}
	return fieldModeNullable
}

func valuesToAny(row map[string]bigquery.Value) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

func intParam(params map[string]any, key string, fallback int) int {
	v, ok := params[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return fallback
	}
}
