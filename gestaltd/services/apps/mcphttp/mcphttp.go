package mcphttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const (
	connectionParam = "_connection"
	instanceParam   = "_instance"
)

type ToolResultEnvelope struct {
	Meta              *mcpgo.Meta     `json:"_meta,omitempty"`
	Content           []mcpgo.Content `json:"content"`
	StructuredContent any             `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

type resultBodyMode string

const (
	resultBodyEnvelope  resultBodyMode = "envelope"
	resultBodyFlattened resultBodyMode = "flattened"
)

func OperationResultFromToolResult(result *mcpgo.CallToolResult) (*core.OperationResult, error) {
	return operationResultFromToolResultWithBody(result, resultBodyEnvelope)
}

func OperationResultFromToolResultForSurface(result *mcpgo.CallToolResult, surface invocation.InvocationSurface) (*core.OperationResult, error) {
	if surface == invocation.InvocationSurfaceHTTP {
		return operationResultFromToolResultWithBody(result, resultBodyEnvelope)
	}
	return operationResultFromToolResultWithBody(result, resultBodyFlattened)
}

func operationResultFromToolResultWithBody(result *mcpgo.CallToolResult, mode resultBodyMode) (*core.OperationResult, error) {
	if mode == resultBodyFlattened {
		return flattenedOperationResultFromToolResult(result)
	}
	body, err := MarshalToolResultEnvelope(result)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	opResult := &core.OperationResult{
		Status:  http.StatusOK,
		Headers: headers,
		Body:    body,
	}
	if result != nil {
		opResult.MCPResult = result
	}
	return opResult, nil
}

func flattenedOperationResultFromToolResult(result *mcpgo.CallToolResult) (*core.OperationResult, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	opResult := &core.OperationResult{
		Status:  http.StatusOK,
		Headers: headers,
		Body:    []byte(`{}`),
	}
	if result == nil {
		return opResult, nil
	}
	opResult.MCPResult = result
	if result.IsError {
		status, body, err := toolErrorResponse(result)
		if err != nil {
			return nil, err
		}
		opResult.Status = status
		opResult.Body = body
		return opResult, nil
	}
	body, err := flattenedToolResultBody(result)
	if err != nil {
		return nil, err
	}
	opResult.Body = body
	return opResult, nil
}

// ToolErrorCode marks an error the upstream tool reported itself, as opposed to
// one raised while reaching it.
const ToolErrorCode = "upstream_tool_error"

// maxToolErrorMessageBytes caps the message lifted out of a tool error so a tool
// that puts a large payload in its error text cannot bloat every error line.
// The payload itself still rides along in details.
const maxToolErrorMessageBytes = 4 << 10

// toolErrorResponse shapes an isError tool result for surfaces that flatten the
// MCP envelope. The tool ran and reported a failure, so the body carries its
// reason in the {"error":{"message","code"}} envelope every SDK already reads,
// and the status describes that failure. A blanket 502 told callers the gateway
// had failed and invited them to retry a request the tool had rejected.
//
// Passing the tool's text through matches how REST upstream error bodies are
// already returned. The text is the upstream's own response to the caller's
// request, not a gateway-internal error.
func toolErrorResponse(result *mcpgo.CallToolResult) (int, []byte, error) {
	text := toolResultText(result)
	details := toolErrorDetails(result, text)
	envelope := map[string]any{
		"error": map[string]any{
			"message": toolErrorMessage(details, text),
			"code":    ToolErrorCode,
		},
	}
	if details != nil {
		envelope["details"] = details
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return 0, nil, err
	}
	return toolErrorStatus(details), body, nil
}

func toolResultText(result *mcpgo.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := mcpgo.AsTextContent(content); ok && strings.TrimSpace(text.Text) != "" {
			parts = append(parts, strings.TrimSpace(text.Text))
		}
	}
	return strings.Join(parts, "\n")
}

// toolErrorDetails returns the tool's structured error payload: its structured
// content, or its text when that text is JSON, as many MCP servers encode it.
func toolErrorDetails(result *mcpgo.CallToolResult, text string) any {
	if result.StructuredContent != nil {
		return result.StructuredContent
	}
	if text == "" || !json.Valid([]byte(text)) {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil
	}
	return parsed
}

func toolErrorMessage(details any, text string) string {
	message := text
	if object, ok := details.(map[string]any); ok {
		if value, ok := object["message"].(string); ok && strings.TrimSpace(value) != "" {
			message = strings.TrimSpace(value)
		}
	}
	if message == "" {
		return "upstream tool reported an error without a message"
	}
	return truncateUTF8(message, maxToolErrorMessageBytes)
}

// toolErrorStatus uses the HTTP status a tool reports for its own upstream call,
// when it reports one. 401 and 407 are not passed through: clients read those as
// the gateway rejecting their own credentials. An upstream 5xx becomes 502,
// which is then accurate. Anything else is 422: the request reached the tool and
// the tool refused it.
func toolErrorStatus(details any) int {
	status := reportedStatus(details)
	switch {
	case status == http.StatusUnauthorized, status == http.StatusProxyAuthRequired:
		return http.StatusUnprocessableEntity
	case status >= 400 && status <= 499:
		return status
	case status >= 500 && status <= 599:
		return http.StatusBadGateway
	default:
		return http.StatusUnprocessableEntity
	}
}

func reportedStatus(details any) int {
	object, ok := details.(map[string]any)
	if !ok {
		return 0
	}
	for _, key := range []string{"statusCode", "status"} {
		switch value := object[key].(type) {
		case float64:
			if value == float64(int(value)) {
				return int(value)
			}
		case int:
			return value
		case json.Number:
			if n, err := value.Int64(); err == nil {
				return int(n)
			}
		}
	}
	return 0
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func flattenedToolResultBody(result *mcpgo.CallToolResult) ([]byte, error) {
	if result.StructuredContent != nil {
		body, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return nil, err
		}
		return body, nil
	}

	if len(result.Content) == 1 {
		if text, ok := mcpgo.AsTextContent(result.Content[0]); ok && json.Valid([]byte(strings.TrimSpace(text.Text))) {
			return []byte(text.Text), nil
		}
	}

	body, err := json.Marshal(map[string]any{"content": result.Content})
	if err != nil {
		return nil, err
	}
	return body, nil
}

func MarshalToolResultEnvelope(result *mcpgo.CallToolResult) ([]byte, error) {
	envelope := ToolResultEnvelope{
		Content: []mcpgo.Content{},
	}
	if result != nil {
		envelope.Meta = result.Meta
		if result.Content != nil {
			envelope.Content = result.Content
		}
		envelope.StructuredContent = result.StructuredContent
		envelope.IsError = result.IsError
	}
	return json.Marshal(envelope)
}

func VisibleOnHTTP(op catalog.CatalogOperation) bool {
	return !UsesReservedHTTPParams(op)
}

func ProjectCatalogOperation(op catalog.CatalogOperation) catalog.CatalogOperation {
	op.Method = http.MethodPost
	op.OutputSchema = OutputSchema(op.OutputSchema)
	return op
}

func ValidateInvocation(op catalog.CatalogOperation, method string) error {
	if !VisibleOnHTTP(op) {
		return fmt.Errorf("operation %q is not exposed on HTTP", op.ID)
	}
	if method != http.MethodPost {
		return methodNotAllowedError{}
	}
	return nil
}

func ValidateHTTPInvocation(transport string, op catalog.CatalogOperation, method string) error {
	if transport != catalog.TransportMCPPassthrough {
		return nil
	}
	return ValidateInvocation(op, method)
}

type methodNotAllowedError struct{}

func (methodNotAllowedError) Error() string { return "MCP operations require POST" }

func (methodNotAllowedError) MethodNotAllowed() bool { return true }

func IsMethodNotAllowed(err error) bool {
	var target interface{ MethodNotAllowed() bool }
	return errors.As(err, &target) && target.MethodNotAllowed()
}

func UsesReservedHTTPParams(op catalog.CatalogOperation) bool {
	for _, param := range op.Parameters {
		if reservedHTTPParamName(param.Name) || reservedHTTPParamName(param.WireName) {
			return true
		}
	}
	if len(op.InputSchema) == 0 {
		return false
	}
	var schema any
	// MCP tools with malformed schemas are hidden from HTTP because the server
	// cannot prove they do not collide with reserved REST control parameters.
	if err := json.Unmarshal(op.InputSchema, &schema); err != nil {
		return true
	}
	return schemaExposesReservedHTTPParams(schema, schema, map[string]struct{}{})
}

func reservedHTTPParamName(name string) bool {
	switch strings.TrimSpace(name) {
	case connectionParam, instanceParam:
		return true
	default:
		return false
	}
}

func schemaExposesReservedHTTPParams(root, schema any, seenRefs map[string]struct{}) bool {
	obj, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	if rawProps, ok := obj["properties"].(map[string]any); ok {
		for name := range rawProps {
			if reservedHTTPParamName(name) {
				return true
			}
		}
	}
	if rawRef, ok := obj["$ref"].(string); ok && strings.TrimSpace(rawRef) != "" {
		ref := strings.TrimSpace(rawRef)
		if _, seen := seenRefs[ref]; seen {
			return false
		}
		target, ok := resolveLocalJSONRef(root, ref)
		if !ok {
			return true
		}
		seenRefs[ref] = struct{}{}
		return schemaExposesReservedHTTPParams(root, target, seenRefs)
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := obj[keyword].([]any)
		if !ok {
			continue
		}
		for _, branch := range branches {
			if schemaExposesReservedHTTPParams(root, branch, seenRefs) {
				return true
			}
		}
	}
	return false
}

func resolveLocalJSONRef(root any, ref string) (any, bool) {
	if ref == "#" {
		return root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	cur := root
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[segment]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			return nil, false
		default:
			return nil, false
		}
	}
	return cur, true
}

func OutputSchema(structuredSchema json.RawMessage) json.RawMessage {
	structured := any(map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	})
	if len(structuredSchema) > 0 && json.Valid(structuredSchema) {
		var parsed any
		if err := json.Unmarshal(structuredSchema, &parsed); err == nil && parsed != nil {
			structured = parsed
		}
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
			"structuredContent": structured,
			"_meta": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			"isError": map[string]any{"type": "boolean"},
		},
		"required":             []string{"content", "isError"},
		"additionalProperties": true,
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	return raw
}
