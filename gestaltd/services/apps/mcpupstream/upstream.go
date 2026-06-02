package mcpupstream

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
	"github.com/valon-technologies/gestalt/server/services/egress"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

const httpTimeout = 30 * time.Second

var (
	_ core.Provider               = (*Upstream)(nil)
	_ core.SessionCatalogProvider = (*Upstream)(nil)
)

const (
	defaultCatalogCacheTTL        = 60 * time.Second
	defaultCatalogCacheMaxEntries = 256
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
	connection  string
	connMode    core.ConnectionMode
	headers     map[string]string
	cat         *catalog.Catalog
	client      mcpclient.MCPClient
	exposure    *operationexposure.Policy
	cache       *catalogDiscoveryCache
	checkEgress func(string) error
}

type Option func(*Upstream)

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

func WithConnectionName(connection string) Option {
	return func(u *Upstream) {
		u.connection = strings.TrimSpace(connection)
	}
}

func WithCatalogCache(ttl time.Duration, maxEntries int) Option {
	return func(u *Upstream) {
		if ttl <= 0 {
			u.cache = nil
			return
		}
		if maxEntries <= 0 {
			maxEntries = defaultCatalogCacheMaxEntries
		}
		u.cache = newCatalogDiscoveryCache(ttl, maxEntries)
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
		headers:     normalizeHeaders(headers),
		cache:       newCatalogDiscoveryCache(defaultCatalogCacheTTL, defaultCatalogCacheMaxEntries),
		checkEgress: checkEgress,
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
func (u *Upstream) ConnectionForOperation(operation string) string {
	if u.connection == "" {
		return ""
	}
	if u.cat != nil {
		if _, ok := catalog.OperationByID(u.cat, operation); !ok {
			return ""
		}
	}
	if _, ok := u.resolveInnerName(operation); !ok {
		return ""
	}
	return u.connection
}
func (u *Upstream) ResolveConnectionForOperation(operation string, _ map[string]any) (string, error) {
	return u.ConnectionForOperation(operation), nil
}
func (u *Upstream) Catalog() *catalog.Catalog { return u.decorateCatalog(u.cat) }

func (u *Upstream) SetDisplayName(s string) { u.display = s }
func (u *Upstream) SetDescription(s string) { u.desc = s }
func (u *Upstream) SetIconSVG(svg string)   { u.iconSVG = svg }

func (u *Upstream) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	return ExecuteTool(ctx, u, operation, params, token)
}

func (u *Upstream) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if u.cache != nil {
		return u.cache.Get(ctx, u.catalogCacheKey(ctx, token), func() (*catalog.Catalog, error) {
			return u.discover(ctx, token)
		})
	}
	return u.discover(ctx, token)
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

func (u *Upstream) FilterOperations(allowed map[string]*operationexposure.OperationOverride) error {
	policy, err := operationexposure.New(allowed)
	if err != nil {
		return err
	}
	if u.cat != nil {
		if err := policy.ValidateCatalog(u.cat); err != nil {
			return err
		}
	}
	u.exposure = policy

	if u.cat == nil || policy == nil {
		return nil
	}

	u.cat = policy.ApplyCatalog(u.cat)
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
			return mergeHeaders(u.headers, authHeaders)
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

func normalizeHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	out := make(map[string]string, len(headers))
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[http.CanonicalHeaderKey(k)] = headers[k]
	}
	return out
}

func mergeHeaders(base, override map[string]string) map[string]string {
	out := normalizeHeaders(base)
	if len(override) == 0 {
		return out
	}
	if out == nil {
		out = make(map[string]string, len(override))
	}
	for k, v := range override {
		out[http.CanonicalHeaderKey(k)] = v
	}
	return out
}

func (u *Upstream) discover(ctx context.Context, token string) (*catalog.Catalog, error) {
	if u.client != nil && u.cat != nil {
		return u.decorateCatalog(u.cat), nil
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
	if err := u.exposure.ValidateCatalog(cat); err != nil {
		return nil, err
	}
	return u.decorateCatalog(u.exposure.ApplyCatalog(cat)), nil
}

func (u *Upstream) catalogCacheKey(ctx context.Context, token string) string {
	partition := core.CatalogCachePartitionFromContext(ctx)
	parts := []string{
		u.name,
		u.url,
		partition.Provider,
		partition.Connection,
		partition.Instance,
		tokenCacheDigest(token),
	}
	return strings.Join(parts, "\x00")
}

type catalogDiscoveryCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	entries    map[string]catalogDiscoveryCacheEntry
	inflight   map[string]*catalogDiscoveryInflight
	sequence   uint64
	now        func() time.Time
}

type catalogDiscoveryCacheEntry struct {
	cat       *catalog.Catalog
	expiresAt time.Time
	sequence  uint64
}

type catalogDiscoveryInflight struct {
	done chan struct{}
	cat  *catalog.Catalog
	err  error
}

func newCatalogDiscoveryCache(ttl time.Duration, maxEntries int) *catalogDiscoveryCache {
	if maxEntries <= 0 {
		maxEntries = defaultCatalogCacheMaxEntries
	}
	return &catalogDiscoveryCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    make(map[string]catalogDiscoveryCacheEntry),
		inflight:   make(map[string]*catalogDiscoveryInflight),
		now:        time.Now,
	}
}

func (c *catalogDiscoveryCache) Get(ctx context.Context, key string, load func() (*catalog.Catalog, error)) (*catalog.Catalog, error) {
	if c == nil || c.ttl <= 0 || c.maxEntries <= 0 {
		return load()
	}

	c.mu.Lock()
	now := c.now()
	if entry, ok := c.entries[key]; ok {
		if now.Before(entry.expiresAt) {
			cat := cloneCatalog(entry.cat)
			c.mu.Unlock()
			return cat, nil
		}
		delete(c.entries, key)
	}
	if flight, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-flight.done:
			return cloneCatalog(flight.cat), flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &catalogDiscoveryInflight{done: make(chan struct{})}
	c.inflight[key] = flight
	c.mu.Unlock()

	cat, err := load()

	c.mu.Lock()
	delete(c.inflight, key)
	flight.cat = cloneCatalog(cat)
	flight.err = err
	if err == nil {
		c.sequence++
		c.entries[key] = catalogDiscoveryCacheEntry{
			cat:       cloneCatalog(cat),
			expiresAt: c.now().Add(c.ttl),
			sequence:  c.sequence,
		}
		c.evictLocked(c.now())
	}
	close(flight.done)
	c.mu.Unlock()

	return cat, err
}

func (c *catalogDiscoveryCache) evictLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
	for len(c.entries) > c.maxEntries {
		var oldestKey string
		var oldestSequence uint64
		for key, entry := range c.entries {
			if oldestKey == "" || entry.sequence < oldestSequence {
				oldestKey = key
				oldestSequence = entry.sequence
			}
		}
		if oldestKey == "" {
			return
		}
		delete(c.entries, oldestKey)
	}
}

func cloneCatalog(cat *catalog.Catalog) *catalog.Catalog {
	if cat == nil {
		return nil
	}
	return cat.Clone()
}

var (
	tokenCacheSecretOnce sync.Once
	tokenCacheSecret     [32]byte
)

func tokenCacheDigest(token string) string {
	if token == "" {
		return ""
	}
	tokenCacheSecretOnce.Do(func() {
		if _, err := rand.Read(tokenCacheSecret[:]); err != nil {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
			tokenCacheSecret = sum
		}
	})
	mac := hmac.New(sha256.New, tokenCacheSecret[:])
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func (u *Upstream) resolveInnerName(name string) (string, bool) {
	return u.exposure.Resolve(name)
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
		catOp.Annotations = catalog.CapabilityAnnotations{
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
