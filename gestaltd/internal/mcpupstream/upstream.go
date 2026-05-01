package mcpupstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/observability"
	"github.com/valon-technologies/gestalt/server/internal/operationexposure"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/singleflight"
)

const (
	httpTimeout                 = 30 * time.Second
	defaultCatalogCacheTTL      = 5 * time.Minute
	defaultCatalogCacheMaxItems = 128
)

var (
	_ core.Provider               = (*Upstream)(nil)
	_ core.SessionCatalogProvider = (*Upstream)(nil)
)

type managedMCPClient struct {
	mcpclient.MCPClient
	onClose func()
}

func (c *managedMCPClient) Close() error {
	err := c.MCPClient.Close()
	if c.onClose != nil {
		c.onClose()
	}
	return err
}

type Upstream struct {
	name        string
	display     string
	desc        string
	iconSVG     string
	url         string
	connMode    core.ConnectionMode
	headers     map[string]string
	cat         *catalog.Catalog
	catMu       sync.RWMutex
	client      mcpclient.MCPClient
	exposure    atomic.Pointer[operationexposure.Policy]
	checkEgress func(string) error

	catalogCacheMu       sync.Mutex
	catalogCache         map[string]cachedCatalog
	catalogCacheGroup    singleflight.Group
	catalogCacheTTL      time.Duration
	catalogCacheMaxItems int
	catalogCacheGen      uint64
}

type Option func(*Upstream)

type cachedCatalog struct {
	cat       *catalog.Catalog
	expiresAt time.Time
}

func WithMetadataOverrides(displayName, description, iconSVG string) Option {
	return func(u *Upstream) {
		if displayName != "" {
			u.display = displayName
		}
		if description != "" {
			u.desc = description
		}
		if iconSVG != "" {
			u.iconSVG = iconSVG
		}
	}
}

func New(_ context.Context, name string, url string, connMode core.ConnectionMode, headers map[string]string, checkEgress func(string) error, opts ...Option) (*Upstream, error) {
	if url == "" {
		return nil, fmt.Errorf("mcpupstream %s: url is required", name)
	}

	u := &Upstream{
		name:        name,
		display:     name,
		desc:        fmt.Sprintf("MCP upstream: %s", url),
		url:         url,
		connMode:    connMode,
		headers:     config.NormalizeHeaders(headers),
		checkEgress: checkEgress,

		catalogCacheTTL:      defaultCatalogCacheTTL,
		catalogCacheMaxItems: defaultCatalogCacheMaxItems,
	}
	for _, opt := range opts {
		opt(u)
	}
	return u, nil
}

func newFromClient(name string, client mcpclient.MCPClient, connMode core.ConnectionMode, tools []mcpgo.Tool) *Upstream {
	cat := buildCatalog(name, tools)
	return &Upstream{
		name:     name,
		display:  name,
		desc:     fmt.Sprintf("MCP upstream: %s", name),
		connMode: connMode,
		cat:      cat,
		client:   client,

		catalogCacheTTL:      defaultCatalogCacheTTL,
		catalogCacheMaxItems: defaultCatalogCacheMaxItems,
	}
}

func (u *Upstream) Name() string                        { return u.name }
func (u *Upstream) DisplayName() string                 { return u.display }
func (u *Upstream) Description() string                 { return u.desc }
func (u *Upstream) ConnectionMode() core.ConnectionMode { return u.connMode }
func (u *Upstream) AuthTypes() []string                 { return nil }
func (u *Upstream) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return nil
}
func (u *Upstream) CredentialFields() []core.CredentialFieldDef { return nil }
func (u *Upstream) DiscoveryConfig() *core.DiscoveryConfig      { return nil }
func (u *Upstream) ConnectionForOperation(string) string        { return "" }
func (u *Upstream) Catalog() *catalog.Catalog                   { return u.decorateCatalog(u.staticCatalog()) }

func (u *Upstream) SetDisplayName(s string) { u.display = s }
func (u *Upstream) SetDescription(s string) { u.desc = s }
func (u *Upstream) SetIconSVG(svg string)   { u.iconSVG = svg }

func (u *Upstream) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return nil, core.ErrMCPOnly
}

func (u *Upstream) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if u.client != nil && u.hasStaticCatalog() {
		return u.discover(ctx, token)
	}

	cacheKey := mcpCatalogCacheKey(token)
	if cat := u.cachedCatalog(cacheKey); cat != nil {
		observability.RecordMCPCatalogCacheHit(ctx, u.catalogMetricAttrs("hit")...)
		return u.decorateCatalog(cat), nil
	}
	observability.RecordMCPCatalogCacheMiss(ctx, u.catalogMetricAttrs("miss")...)

	cacheGen := u.catalogCacheGeneration()
	flightKey := fmt.Sprintf("%s:%d", cacheKey, cacheGen)
	result := u.catalogCacheGroup.DoChan(flightKey, func() (any, error) {
		if cat := u.cachedCatalog(cacheKey); cat != nil {
			return cat, nil
		}

		discoverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), httpTimeout)
		defer cancel()
		startedAt := time.Now()
		cat, err := u.discoverCatalog(discoverCtx, token)
		observability.RecordMCPCatalogDiscover(discoverCtx, startedAt, err != nil, u.catalogMetricAttrs("")...)
		if err != nil {
			return nil, err
		}
		u.storeCachedCatalog(cacheKey, cacheGen, cat)
		return cat, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-result:
		if res.Err != nil {
			return nil, res.Err
		}
		cat, ok := res.Val.(*catalog.Catalog)
		if !ok || cat == nil {
			return nil, fmt.Errorf("mcpupstream %s: catalog discovery returned no catalog", u.name)
		}
		return u.decorateCatalog(cat), nil
	}
}

func (u *Upstream) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	innerName, ok := u.resolveInnerName(name)
	if !ok {
		return nil, fmt.Errorf("operation %q is not allowed", name)
	}
	req := mcpgo.CallToolRequest{}
	req.Params.Name = innerName
	req.Params.Arguments = args
	req.Params.Meta = CallToolMetaFromContext(ctx)

	if u.client != nil {
		return u.client.CallTool(ctx, req)
	}

	client, err := u.connect(ctx, UpstreamTokenFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	return client.CallTool(ctx, req)
}

func (u *Upstream) Close() error {
	if u.client == nil {
		return nil
	}
	return u.client.Close()
}

func (u *Upstream) FilterOperations(allowed map[string]*config.OperationOverride) error {
	policy, err := operationexposure.New(allowed)
	if err != nil {
		return err
	}

	u.catMu.Lock()
	if u.cat != nil {
		if err := policy.ValidateCatalog(u.cat); err != nil {
			u.catMu.Unlock()
			return err
		}
		if policy != nil {
			u.cat = policy.ApplyCatalog(u.cat)
		}
	}
	u.catMu.Unlock()

	u.exposure.Store(policy)
	u.clearCatalogCache()
	return nil
}

func (u *Upstream) connect(ctx context.Context, token string) (mcpclient.MCPClient, error) {
	if u.client != nil {
		return u.client, nil
	}

	if u.checkEgress != nil {
		parsed, err := neturl.Parse(u.url)
		if err != nil {
			return nil, fmt.Errorf("mcpupstream %s: parsing url for egress check: %w", u.name, err)
		}
		if err := u.checkEgress(parsed.Host); err != nil {
			return nil, err
		}
	}

	baseTransport := egress.CloneDefaultTransport()
	httpClient := &http.Client{
		Timeout:   httpTimeout,
		Transport: baseTransport,
	}
	closeIdleConnections := func() { baseTransport.CloseIdleConnections() }

	client, err := mcpclient.NewStreamableHttpClient(u.url,
		transport.WithHTTPBasicClient(httpClient),
		transport.WithHTTPHeaderFunc(func(context.Context) map[string]string {
			var authHeaders map[string]string
			if token != "" {
				authHeaders = map[string]string{"Authorization": core.BearerScheme + token}
			}
			return config.MergeHeaders(u.headers, authHeaders)
		}),
	)
	if err != nil {
		closeIdleConnections()
		return nil, fmt.Errorf("mcpupstream %s: creating client: %w", u.name, err)
	}

	if err := client.Start(ctx); err != nil {
		_ = client.Close()
		closeIdleConnections()
		return nil, fmt.Errorf("mcpupstream %s: starting client: %w", u.name, err)
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "gestalt", Version: "0.1.0"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		closeIdleConnections()
		return nil, fmt.Errorf("mcpupstream %s: initialize: %w", u.name, err)
	}

	return &managedMCPClient{MCPClient: client, onClose: closeIdleConnections}, nil
}

func (u *Upstream) discover(ctx context.Context, token string) (*catalog.Catalog, error) {
	cat, err := u.discoverCatalog(ctx, token)
	if err != nil {
		return nil, err
	}
	return u.decorateCatalog(cat), nil
}

func (u *Upstream) discoverCatalog(ctx context.Context, token string) (*catalog.Catalog, error) {
	if u.client != nil {
		if cat := u.staticCatalog(); cat != nil {
			return cat, nil
		}
	}

	client, err := u.connect(ctx, token)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("mcpupstream %s: listing tools: %w", u.name, err)
	}

	cat := buildCatalog(u.name, toolsResult.Tools)
	exposure := u.exposure.Load()
	if err := exposure.ValidateCatalog(cat); err != nil {
		return nil, err
	}
	return exposure.ApplyCatalog(cat), nil
}

func (u *Upstream) resolveInnerName(name string) (string, bool) {
	return u.exposure.Load().Resolve(name)
}

func buildCatalog(name string, tools []mcpgo.Tool) *catalog.Catalog {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(tools)),
	}

	for i := range tools {
		schema, _ := json.Marshal(tools[i].InputSchema)

		var outputSchema json.RawMessage
		if tools[i].OutputSchema.Type != "" {
			outputSchema, _ = json.Marshal(tools[i].OutputSchema)
		}

		catOp := catalog.CatalogOperation{
			ID:           tools[i].Name,
			Title:        tools[i].Annotations.Title,
			Description:  tools[i].Description,
			InputSchema:  schema,
			OutputSchema: outputSchema,
			Transport:    catalog.TransportMCPPassthrough,
		}
		catOp.Annotations = catalog.OperationAnnotations{
			ReadOnlyHint:    tools[i].Annotations.ReadOnlyHint,
			DestructiveHint: tools[i].Annotations.DestructiveHint,
			IdempotentHint:  tools[i].Annotations.IdempotentHint,
			OpenWorldHint:   tools[i].Annotations.OpenWorldHint,
		}
		cat.Operations = append(cat.Operations, catOp)
	}

	return cat
}

func (u *Upstream) decorateCatalog(cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		if u.iconSVG == "" {
			return nil
		}
		return &catalog.Catalog{
			Name:        u.name,
			DisplayName: u.display,
			Description: u.desc,
			IconSVG:     u.iconSVG,
		}
	}
	decorated := cat.Clone()
	decorated.DisplayName = u.display
	decorated.Description = u.desc
	if u.iconSVG != "" {
		decorated.IconSVG = u.iconSVG
	}
	return decorated
}

func (u *Upstream) staticCatalog() *catalog.Catalog {
	u.catMu.RLock()
	defer u.catMu.RUnlock()
	if u.cat == nil {
		return nil
	}
	return u.cat.Clone()
}

func (u *Upstream) hasStaticCatalog() bool {
	u.catMu.RLock()
	defer u.catMu.RUnlock()
	return u.cat != nil
}

func (u *Upstream) cachedCatalog(key string) *catalog.Catalog {
	if u.catalogCacheTTL <= 0 {
		return nil
	}

	now := time.Now()
	u.catalogCacheMu.Lock()
	defer u.catalogCacheMu.Unlock()
	cached, ok := u.catalogCache[key]
	if !ok {
		return nil
	}
	if !cached.expiresAt.After(now) {
		delete(u.catalogCache, key)
		return nil
	}
	return cached.cat.Clone()
}

func (u *Upstream) storeCachedCatalog(key string, generation uint64, cat *catalog.Catalog) {
	if u.catalogCacheTTL <= 0 || cat == nil {
		return
	}

	u.catalogCacheMu.Lock()
	defer u.catalogCacheMu.Unlock()
	if u.catalogCacheGen != generation {
		return
	}
	if u.catalogCache == nil {
		u.catalogCache = make(map[string]cachedCatalog)
	}
	u.pruneCatalogCacheLocked(time.Now())
	if maxItems := u.catalogCacheMaxItems; maxItems > 0 && len(u.catalogCache) >= maxItems {
		for existingKey := range u.catalogCache {
			delete(u.catalogCache, existingKey)
			break
		}
	}
	u.catalogCache[key] = cachedCatalog{
		cat:       cat.Clone(),
		expiresAt: time.Now().Add(u.catalogCacheTTL),
	}
}

func (u *Upstream) clearCatalogCache() {
	u.catalogCacheMu.Lock()
	defer u.catalogCacheMu.Unlock()
	u.catalogCacheGen++
	clear(u.catalogCache)
}

func (u *Upstream) catalogCacheGeneration() uint64 {
	u.catalogCacheMu.Lock()
	defer u.catalogCacheMu.Unlock()
	return u.catalogCacheGen
}

func (u *Upstream) pruneCatalogCacheLocked(now time.Time) {
	for key, cached := range u.catalogCache {
		if !cached.expiresAt.After(now) {
			delete(u.catalogCache, key)
		}
	}
}

func (u *Upstream) catalogMetricAttrs(cacheResult string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		metricutil.AttrProvider.String(metricutil.AttrValue(u.name)),
	}
	if cacheResult != "" {
		attrs = append(attrs, observability.AttrMCPCatalogCacheResult.String(cacheResult))
	}
	return attrs
}

func mcpCatalogCacheKey(token string) string {
	if token == "" {
		return "no-token"
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
