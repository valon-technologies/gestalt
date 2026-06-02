package mcpupstream

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/apps/mcphttp"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// ToolCaller is the minimal MCP tool execution contract shared by upstreams
// and tests that model upstream behavior.
type ToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcpgo.CallToolResult, error)
}

// ExecuteTool applies upstream token context, calls the MCP tool, and shapes the
// result body for the current invocation surface.
func ExecuteTool(ctx context.Context, caller ToolCaller, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	if token != "" {
		ctx = WithUpstreamToken(ctx, token)
	}
	result, err := caller.CallTool(ctx, operation, params)
	if err != nil {
		return nil, err
	}
	return mcphttp.OperationResultFromToolResultForSurface(result, invocation.InvocationSurfaceFromContext(ctx))
}
