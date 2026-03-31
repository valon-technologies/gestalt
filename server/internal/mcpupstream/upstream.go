package mcpupstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/operationexposure"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

const httpTimeout = 30 * time.Second

var (
	_ core.Provider               = (*Upstream)(nil)
	_ core.CatalogProvider        = (*Upstream)(nil)
	_ core.SessionCatalogProvider = (*Upstream)(nil)
	_ core.ManualProvider         = (*Upstream)(nil)
)

type Upstream struct {
	name     string
	display  string
	desc     string
	url      string
	connMode core.ConnectionMode
	headers  map[string]string
	cat      *catalog.Catalog
	ops      []core.Operation
	client   mcpclient.MCPClient
	exposure *operationexposure.Policy
	resolver *egress.Resolver
}

func New(_ context.Context, name string, url string, connMode core.ConnectionMode, headers map[string]string, resolver *egress.Resolver) (*Upstream, error) {
	if url == "" {
		return nil, fmt.Errorf("mcpupstream %s: url is required", name)
	}

	return &Upstream{
		name:     name,
		display:  name,
		desc:     fmt.Sprintf("MCP upstream: %s", url),
		url:      url,
		connMode: connMode,
		headers:  config.NormalizeHeaders(headers),
		resolver: resolver,
	}, nil
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
func (u *Upstream) ListOperations() []core.Operation    { return slices.Clone(u.ops) }
func (u *Upstream) Catalog() *catalog.Catalog           { return u.cat }
func (u *Upstream) SupportsManualAuth() bool            { return true }

func (u *Upstream) SetDisplayName(s string) { u.display = s }
func (u *Upstream) SetDescription(s string) { u.desc = s }
func (u *Upstream) SetIconSVG(svg string) {
	if u.cat != nil {
		u.cat.IconSVG = svg
	}
}

func (u *Upstream) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return nil, core.ErrMCPOnly
}

func (u *Upstream) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	cat, _, err := u.discover(ctx, token)
	if err != nil {
		return nil, err
	}
	return cat, nil
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
	if u.cat != nil {
		if err := policy.Validate(u.ops); err != nil {
			return err
		}
	}
	u.exposure = policy

	if u.cat == nil || policy == nil {
		return nil
	}

	u.ops = policy.ApplyOperations(u.ops)
	u.cat = policy.ApplyCatalog(u.cat)
	return nil
}

func (u *Upstream) connect(ctx context.Context, token string) (mcpclient.MCPClient, error) {
	if u.client != nil {
		return u.client, nil
	}

	httpClient := &http.Client{
		Timeout:   httpTimeout,
		Transport: egress.NewResolvingRoundTripper(http.DefaultTransport, u.resolver),
	}

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
		return nil, fmt.Errorf("mcpupstream %s: creating client: %w", u.name, err)
	}

	if err := client.Start(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcpupstream %s: starting client: %w", u.name, err)
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "gestalt", Version: "0.1.0"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcpupstream %s: initialize: %w", u.name, err)
	}

	return client, nil
}

func (u *Upstream) discover(ctx context.Context, token string) (*catalog.Catalog, []core.Operation, error) {
	if u.client != nil && u.cat != nil {
		return u.cat.Clone(), slices.Clone(u.ops), nil
	}

	client, err := u.connect(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = client.Close() }()

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		return nil, nil, fmt.Errorf("mcpupstream %s: listing tools: %w", u.name, err)
	}

	cat, ops := buildCatalog(u.name, toolsResult.Tools)
	if err := u.exposure.Validate(ops); err != nil {
		return nil, nil, err
	}
	ops = u.exposure.ApplyOperations(ops)
	cat = u.exposure.ApplyCatalog(cat)
	return cat, ops, nil
}

func (u *Upstream) resolveInnerName(name string) (string, bool) {
	return u.exposure.Resolve(name)
}

func buildCatalog(name string, tools []mcpgo.Tool) (*catalog.Catalog, []core.Operation) {
	cat := &catalog.Catalog{
		Name:       name,
		Operations: make([]catalog.CatalogOperation, 0, len(tools)),
	}
	ops := make([]core.Operation, 0, len(tools))

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

		ops = append(ops, core.Operation{
			Name:        tools[i].Name,
			Description: tools[i].Description,
		})
	}

	return cat, ops
}
