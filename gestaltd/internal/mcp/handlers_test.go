package mcp

import (
	"context"
	"fmt"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type stubInvoker struct {
	result *core.OperationResult
	err    error
}

func (s stubInvoker) Invoke(context.Context, *principal.Principal, string, string, string, map[string]any) (*core.OperationResult, error) {
	return s.result, s.err
}

func TestMakeHandlerSanitizesInternalErrors(t *testing.T) {
	handler := makeHandler(stubInvoker{err: fmt.Errorf("%w: database DSN leaked", invocation.ErrInternal)}, "demo", "op", "")

	result, err := handler(testPrincipalContext(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Arguments: map[string]any{}},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error result")
	}
	if text := toolResultText(t, result); text != "internal error" {
		t.Fatalf("unexpected tool error text %q", text)
	}
}

func TestMakeHandlerPreservesAccessDeniedErrors(t *testing.T) {
	handler := makeHandler(stubInvoker{err: invocation.ErrAuthorizationDenied}, "demo", "op", "")

	result, err := handler(testPrincipalContext(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Arguments: map[string]any{}},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error result")
	}
	if text := toolResultText(t, result); text != "operation access denied" {
		t.Fatalf("unexpected tool error text %q", text)
	}
}

func TestMakeHandlerPreservesNoTokenErrors(t *testing.T) {
	handler := makeHandler(stubInvoker{err: fmt.Errorf("%w: no token stored for integration %q", invocation.ErrNoToken, "demo")}, "demo", "op", "")

	result, err := handler(testPrincipalContext(), mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Arguments: map[string]any{}},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error result")
	}
	if text := toolResultText(t, result); text != `no integration token: no token stored for integration "demo"` {
		t.Fatalf("unexpected tool error text %q", text)
	}
}

func testPrincipalContext() context.Context {
	return principal.WithPrincipal(context.Background(), &principal.Principal{
		UserID: "user-1",
		Source: principal.SourceAPIToken,
	})
}

func toolResultText(t *testing.T, result *mcpgo.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("expected one content item, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}
