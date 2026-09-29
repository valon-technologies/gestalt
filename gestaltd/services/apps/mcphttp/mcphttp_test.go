package mcphttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// The payload Atlassian's MCP server returns when Jira rejects a create, as
// observed through the gateway: JSON in a text content item with statusCode.
const jiraCreateRejected = `{"error":true,"message":"Failed to create issue: Bad Request. The target project doesn't exist or you don't have permission to create issues in it.","problems":[{"field":"project","reason":"The target project doesn't exist or you don't have permission to create issues in it."}],"statusCode":400}`

func toolError(content ...mcpgo.Content) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{Content: content, IsError: true}
}

func flattenedError(t *testing.T, result *mcpgo.CallToolResult) (int, map[string]any) {
	t.Helper()
	opResult, err := OperationResultFromToolResultForSurface(result, invocation.InvocationSurface(""))
	if err != nil {
		t.Fatalf("OperationResultFromToolResultForSurface: %v", err)
	}
	if opResult.MCPResult != result {
		t.Fatalf("MCPResult was not preserved")
	}
	var body map[string]any
	if err := json.Unmarshal(opResult.Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, opResult.Body)
	}
	return opResult.Status, body
}

func errorEnvelope(t *testing.T, body map[string]any) (message, code string) {
	t.Helper()
	nested, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("error is not an object: %#v", body)
	}
	message, _ = nested["message"].(string)
	code, _ = nested["code"].(string)
	return message, code
}

func TestFlattenedToolErrorCarriesUpstreamReasonAndStatus(t *testing.T) {
	t.Parallel()

	status, body := flattenedError(t, toolError(mcpgo.NewTextContent(jiraCreateRejected)))

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 from the tool's statusCode", status)
	}
	message, code := errorEnvelope(t, body)
	if !strings.Contains(message, "The target project doesn't exist") {
		t.Fatalf("message = %q, want the upstream reason", message)
	}
	if code != ToolErrorCode {
		t.Fatalf("code = %q, want %q", code, ToolErrorCode)
	}
	details, ok := body["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing: %#v", body)
	}
	if problems, _ := details["problems"].([]any); len(problems) != 1 {
		t.Fatalf("details.problems = %#v, want the tool's field problems", details["problems"])
	}
}

func TestFlattenedToolErrorWithoutReportedStatusIs422(t *testing.T) {
	t.Parallel()

	status, body := flattenedError(t, toolError(mcpgo.NewTextContent(`{"message":"channel_not_found"}`)))

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if message, _ := errorEnvelope(t, body); message != "channel_not_found" {
		t.Fatalf("message = %q, want channel_not_found", message)
	}
}

func TestFlattenedToolErrorWithPlainText(t *testing.T) {
	t.Parallel()

	status, body := flattenedError(t, toolError(
		mcpgo.NewTextContent("Page not found"),
		mcpgo.NewTextContent("Check the page id"),
	))

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if message, _ := errorEnvelope(t, body); message != "Page not found\nCheck the page id" {
		t.Fatalf("message = %q, want the joined text", message)
	}
	if _, ok := body["details"]; ok {
		t.Fatalf("details should be omitted for non-JSON text: %#v", body)
	}
}

func TestFlattenedToolErrorPrefersStructuredContent(t *testing.T) {
	t.Parallel()

	result := toolError(mcpgo.NewTextContent("not the message"))
	result.StructuredContent = map[string]any{"message": "quota exhausted", "statusCode": float64(http.StatusTooManyRequests)}

	status, body := flattenedError(t, result)

	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", status)
	}
	if message, _ := errorEnvelope(t, body); message != "quota exhausted" {
		t.Fatalf("message = %q, want the structured message", message)
	}
}

func TestFlattenedToolErrorStatusMapping(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		reported string
		want     int
	}{
		{name: "not found passes through", reported: `404`, want: http.StatusNotFound},
		{name: "upstream 5xx is a bad gateway", reported: `503`, want: http.StatusBadGateway},
		{name: "401 is not read as gateway auth", reported: `401`, want: http.StatusUnprocessableEntity},
		{name: "407 is not read as gateway auth", reported: `407`, want: http.StatusUnprocessableEntity},
		{name: "non-error status", reported: `200`, want: http.StatusUnprocessableEntity},
		{name: "string status is ignored", reported: `"error"`, want: http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, _ := flattenedError(t, toolError(mcpgo.NewTextContent(`{"message":"x","status":`+tc.reported+`}`)))
			if status != tc.want {
				t.Fatalf("status = %d, want %d", status, tc.want)
			}
		})
	}
}

func TestFlattenedToolErrorWithoutContent(t *testing.T) {
	t.Parallel()

	status, body := flattenedError(t, toolError())

	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if message, _ := errorEnvelope(t, body); message == "" {
		t.Fatal("message is empty; want a fallback")
	}
}

func TestFlattenedToolErrorTruncatesLongMessages(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", maxToolErrorMessageBytes)
	_, body := flattenedError(t, toolError(mcpgo.NewTextContent(long)))

	message, _ := errorEnvelope(t, body)
	if len(message) > maxToolErrorMessageBytes+len("…") {
		t.Fatalf("message is %d bytes, want at most %d", len(message), maxToolErrorMessageBytes)
	}
	if !utf8.ValidString(message) {
		t.Fatal("truncation split a UTF-8 sequence")
	}
}

func TestHTTPSurfaceKeepsToolErrorEnvelope(t *testing.T) {
	t.Parallel()

	result := toolError(mcpgo.NewTextContent(jiraCreateRejected))
	opResult, err := OperationResultFromToolResultForSurface(result, invocation.InvocationSurfaceHTTP)
	if err != nil {
		t.Fatalf("OperationResultFromToolResultForSurface: %v", err)
	}
	if opResult.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for the HTTP envelope", opResult.Status)
	}
	var body map[string]any
	if err := json.Unmarshal(opResult.Body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["isError"] != true {
		t.Fatalf("isError = %v, want true", body["isError"])
	}
}
