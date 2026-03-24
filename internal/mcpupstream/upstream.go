package mcpupstream

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

const httpTimeout = 30 * time.Second

var (
	_ core.Provider        = (*Upstream)(nil)
	_ core.CatalogProvider = (*Upstream)(nil)
	_ core.ManualProvider  = (*Upstream)(nil)
)

type Upstream struct {
	name     string
	display  string
	desc     string
	connMode core.ConnectionMode
	cat      *catalog.Catalog
	ops      []core.Operation
	client   mcpclient.MCPClient

	// Deferred initialization fields. When deferred is true, the upstream
	// was created without connecting; EnsureInitialized must be called
	// with an authenticated context before the client can be used.
	url        string
	deferred   bool
	mu         sync.RWMutex
	allowedOps map[string]string
}

func New(ctx context.Context, name string, url string, connMode core.ConnectionMode) (*Upstream, error) {
	if url == "" {
		return nil, fmt.Errorf("mcpupstream %s: url is required", name)
	}

	client, tools, err := connect(ctx, name, url)
	if err != nil {
		return nil, err
	}

	cat, ops := buildCatalog(name, tools)

	return &Upstream{
		name:     name,
		display:  name,
		desc:     fmt.Sprintf("MCP upstream: %s", url),
		connMode: connMode,
		cat:      cat,
		ops:      ops,
		client:   client,
	}, nil
}

func NewDeferred(name string, url string, connMode core.ConnectionMode) *Upstream {
	return &Upstream{
		name:     name,
		display:  name,
		desc:     fmt.Sprintf("MCP upstream: %s", url),
		connMode: connMode,
		url:      url,
		deferred: true,
		cat:      &catalog.Catalog{Name: name},
	}
}

func (u *Upstream) SetAllowedOperations(allowed map[string]string) {
	u.allowedOps = allowed
}

func (u *Upstream) IsDeferred() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.deferred
}

// EnsureInitialized connects a deferred upstream using the token present in
// ctx. Safe to call concurrently; failed attempts allow retry on next call.
// Returns true when this call performed the actual initialization, false when
// the upstream was already initialized (by this or another goroutine).
func (u *Upstream) EnsureInitialized(ctx context.Context) (bool, error) {
	u.mu.RLock()
	if !u.deferred {
		u.mu.RUnlock()
		return false, nil
	}
	u.mu.RUnlock()

	client, tools, err := connect(ctx, u.name, u.url)
	if err != nil {
		return false, err
	}

	cat, ops := buildCatalog(u.name, tools)

	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.deferred {
		_ = client.Close()
		return false, nil
	}

	u.cat = cat
	u.ops = ops
	if u.allowedOps != nil {
		if err := u.FilterOperations(u.allowedOps); err != nil {
			u.cat = &catalog.Catalog{Name: u.name}
			u.ops = nil
			_ = client.Close()
			return false, fmt.Errorf("mcpupstream %s: %w", u.name, err)
		}
	}

	u.client = client
	u.deferred = false
	return true, nil
}

func connect(ctx context.Context, name, url string) (mcpclient.MCPClient, []mcpgo.Tool, error) {
	client, err := mcpclient.NewStreamableHttpClient(url,
		transport.WithHTTPTimeout(httpTimeout),
		transport.WithHTTPHeaderFunc(func(ctx context.Context) map[string]string {
			if token := UpstreamTokenFromContext(ctx); token != "" {
				return map[string]string{"Authorization": core.BearerScheme + token}
			}
			return nil
		}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("mcpupstream %s: creating client: %w", name, err)
	}

	if err := client.Start(ctx); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("mcpupstream %s: starting client: %w", name, err)
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "gestalt", Version: "0.1.0"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("mcpupstream %s: initialize: %w", name, err)
	}

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("mcpupstream %s: listing tools: %w", name, err)
	}

	return client, toolsResult.Tools, nil
}

func newFromClient(name string, client mcpclient.MCPClient, connMode core.ConnectionMode, tools []mcpgo.Tool) *Upstream {
	cat, ops := buildCatalog(name, tools)
	return &Upstream{
		name:     name,
		display:  name,
		desc:     fmt.Sprintf("MCP upstream: %s", name),
		connMode: connMode,
		cat:      cat,
		ops:      ops,
		client:   client,
	}
}

func (u *Upstream) Name() string                        { return u.name }
func (u *Upstream) DisplayName() string                 { return u.display }
func (u *Upstream) Description() string                 { return u.desc }
func (u *Upstream) ConnectionMode() core.ConnectionMode { return u.connMode }
func (u *Upstream) SupportsManualAuth() bool            { return true }

func (u *Upstream) ListOperations() []core.Operation {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.ops
}

func (u *Upstream) Catalog() *catalog.Catalog {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.cat
}

func (u *Upstream) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return nil, core.ErrMCPOnly
}

func (u *Upstream) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	if _, err := u.EnsureInitialized(ctx); err != nil {
		return nil, fmt.Errorf("deferred init: %w", err)
	}
	u.mu.RLock()
	c := u.client
	u.mu.RUnlock()
	req := mcpgo.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return c.CallTool(ctx, req)
}

func (u *Upstream) Close() error {
	u.mu.RLock()
	c := u.client
	u.mu.RUnlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

func (u *Upstream) FilterOperations(allowed map[string]string) error {
	if len(allowed) == 0 {
		return fmt.Errorf("allowed_operations cannot be empty; omit the field to allow all")
	}

	opSet := make(map[string]struct{}, len(u.ops))
	for _, op := range u.ops {
		opSet[op.Name] = struct{}{}
	}
	for name := range allowed {
		if _, ok := opSet[name]; !ok {
			return fmt.Errorf("allowed_operations contains unknown operation %q", name)
		}
	}

	filteredOps := make([]core.Operation, 0, len(allowed))
	for _, op := range u.ops {
		if desc, ok := allowed[op.Name]; ok {
			if desc != "" {
				op.Description = desc
			}
			filteredOps = append(filteredOps, op)
		}
	}

	filteredCatOps := make([]catalog.CatalogOperation, 0, len(allowed))
	for i := range u.cat.Operations {
		if desc, ok := allowed[u.cat.Operations[i].ID]; ok {
			if desc != "" {
				u.cat.Operations[i].Description = desc
			}
			filteredCatOps = append(filteredCatOps, u.cat.Operations[i])
		}
	}

	u.ops = filteredOps
	u.cat.Operations = filteredCatOps
	return nil
}

func buildCatalog(name string, tools []mcpgo.Tool) (*catalog.Catalog, []core.Operation) {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(tools)),
	}
	ops := make([]core.Operation, 0, len(tools))

	for i := range tools {
		schema, _ := json.Marshal(tools[i].InputSchema)

		catOp := catalog.CatalogOperation{
			ID:          tools[i].Name,
			Title:       tools[i].Annotations.Title,
			Description: tools[i].Description,
			InputSchema: schema,
		}
		catOp.Annotations = catalog.OperationAnnotations{
			ReadOnlyHint:    tools[i].Annotations.ReadOnlyHint,
			DestructiveHint: tools[i].Annotations.DestructiveHint,
			IdempotentHint:  tools[i].Annotations.IdempotentHint,
			OpenWorldHint:   tools[i].Annotations.OpenWorldHint,
		}
		cat.Operations = append(cat.Operations, catOp)

		ops = append(ops, core.Operation{
			Name:        tools[i].Name,
			Description: tools[i].Description,
		})
	}

	return cat, ops
}
