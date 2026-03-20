package mcpupstream

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/config"

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
}

func New(ctx context.Context, name string, intg config.IntegrationDef) (*Upstream, error) {
	if intg.MCP == nil || intg.MCP.URL == "" {
		return nil, fmt.Errorf("mcpupstream %s: mcp.url is required", name)
	}

	client, err := mcpclient.NewStreamableHttpClient(intg.MCP.URL,
		transport.WithHTTPTimeout(httpTimeout),
		transport.WithHTTPHeaderFunc(func(ctx context.Context) map[string]string {
			if token := UpstreamTokenFromContext(ctx); token != "" {
				return map[string]string{"Authorization": core.BearerScheme + token}
			}
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("mcpupstream %s: creating client: %w", name, err)
	}

	if err := client.Start(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcpupstream %s: starting client: %w", name, err)
	}

	initReq := mcpgo.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgo.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgo.Implementation{Name: "toolshed", Version: "0.1.0"}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcpupstream %s: initialize: %w", name, err)
	}

	toolsResult, err := client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcpupstream %s: listing tools: %w", name, err)
	}

	cat, ops := buildCatalog(name, toolsResult.Tools)

	connMode := core.ConnectionModeUser
	if intg.ConnectionMode != "" {
		connMode = core.ConnectionMode(intg.ConnectionMode)
	}

	return &Upstream{
		name:     name,
		display:  name,
		desc:     fmt.Sprintf("MCP upstream: %s", intg.MCP.URL),
		connMode: connMode,
		cat:      cat,
		ops:      ops,
		client:   client,
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
func (u *Upstream) ListOperations() []core.Operation    { return u.ops }
func (u *Upstream) Catalog() *catalog.Catalog           { return u.cat }
func (u *Upstream) SupportsManualAuth() bool            { return true }

func (u *Upstream) Execute(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
	return nil, core.ErrMCPOnly
}

func (u *Upstream) CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error) {
	req := mcpgo.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return u.client.CallTool(ctx, req)
}

func (u *Upstream) Close() error {
	return u.client.Close()
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
