package mcphttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
		opResult.Status = http.StatusBadGateway
		opResult.Body = []byte(`{"error":"operation failed"}`)
		return opResult, nil
	}
	body, err := flattenedToolResultBody(result)
	if err != nil {
		return nil, err
	}
	opResult.Body = body
	return opResult, nil
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
		if _, seen := seenRefs[ref]; !seen {
			if target, ok := resolveLocalJSONRef(root, ref); ok {
				seenRefs[ref] = struct{}{}
				if schemaExposesReservedHTTPParams(root, target, seenRefs) {
					return true
				}
			}
		}
	}
	for _, keyword := range []string{"$defs", "definitions"} {
		defs, ok := obj[keyword].(map[string]any)
		if !ok {
			continue
		}
		for _, def := range defs {
			if schemaExposesReservedHTTPParams(root, def, seenRefs) {
				return true
			}
		}
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
