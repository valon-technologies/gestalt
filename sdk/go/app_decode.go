package gestalt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func decodeAppOperationResult(app, operation string, result *OperationResult) (any, error) {
	if result == nil {
		return map[string]any{}, nil
	}
	return decodeAppBody(app, operation, result.Status, result.Body)
}

func decodeAppGraphQLResult(app string, result *OperationResult) (any, error) {
	decoded, err := decodeAppOperationResult(app, "graphql", result)
	if err != nil {
		return nil, err
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return decoded, nil
	}
	errorsValue, ok := object["errors"]
	if !ok {
		return decoded, nil
	}
	errors, ok := errorsValue.([]any)
	if !ok || len(errors) == 0 {
		return decoded, nil
	}
	return nil, &InvokeError{
		App:       app,
		Operation: "graphql",
		Code:      "graphql_errors",
		Message:   graphqlErrorMessage(errors),
		Body:      object,
		RawBody:   result.Body,
	}
}

// InvokeAs invokes an app operation and decodes the response into T.
func InvokeAs[T any](ctx context.Context, client App, app string, operation string, params any, opts *InvokeOptions) (T, error) {
	var out T
	result, err := client.InvokeRaw(ctx, app, operation, params, opts)
	if err != nil {
		return out, err
	}
	if err := DecodeAppResultAs(result, app, operation, &out); err != nil {
		return out, err
	}
	return out, nil
}

// InvokeGraphQLAs invokes a GraphQL surface and decodes the response into T.
func InvokeGraphQLAs[T any](ctx context.Context, client App, app string, document string, variables any, opts *InvokeGraphQLOptions) (T, error) {
	var out T
	result, err := client.InvokeGraphQLRaw(ctx, app, document, variables, opts)
	if err != nil {
		return out, err
	}
	decoded, err := decodeAppGraphQLResult(app, result)
	if err != nil {
		return out, err
	}
	if err := decodeValueAs(decoded, &out); err != nil {
		return out, err
	}
	return out, nil
}

// DecodeAppResultAs decodes a raw app invocation result into T.
func DecodeAppResultAs[T any](result *OperationResult, app string, operation string, out *T) error {
	decoded, err := decodeAppOperationResult(app, operation, result)
	if err != nil {
		return err
	}
	return decodeValueAs(decoded, out)
}

func decodeValueAs[T any](decoded any, out *T) error {
	data, err := json.Marshal(decoded)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	return nil
}

func decodeAppBody(app, operation string, status int, body string) (any, error) {
	parsed, err := parseOperationResultJSON(body)
	if status >= 400 {
		invokeErr := &InvokeError{
			App:       app,
			Operation: operation,
			Status:    status,
			Message:   fmt.Sprintf("app invoke failed with status %d", status),
			RawBody:   body,
		}
		if err == nil {
			invokeErr.Body = parsed
			applyInvokeErrorMessage(invokeErr, parsed)
		}
		return nil, invokeErr
	}
	if err != nil {
		return nil, &InvokeError{
			App:       app,
			Operation: operation,
			Message:   "app invoke response is not valid JSON",
			RawBody:   body,
		}
	}
	if object, ok := parsed.(map[string]any); ok {
		if statusValue, ok := object["status"].(string); ok {
			switch statusValue {
			case "error":
				invokeErr := &InvokeError{
					App:       app,
					Operation: operation,
					Message:   "app invoke failed",
					Body:      parsed,
					RawBody:   body,
				}
				applyInvokeErrorMessage(invokeErr, parsed)
				return nil, invokeErr
			case "success":
				if data, ok := object["data"]; ok {
					return data, nil
				}
			}
		}
	}
	return parsed, nil
}

func parseOperationResultJSON(body string) (any, error) {
	if strings.TrimSpace(body) == "" {
		return map[string]any{}, nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(body))
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err == nil || !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("response body contains trailing JSON")
	}
	return value, nil
}

func applyInvokeErrorMessage(err *InvokeError, parsed any) {
	object, ok := parsed.(map[string]any)
	if !ok {
		return
	}
	if nested, ok := object["error"].(map[string]any); ok {
		if message, ok := nested["message"].(string); ok && strings.TrimSpace(message) != "" {
			err.Message = message
		}
		if code, ok := nested["code"].(string); ok && strings.TrimSpace(code) != "" {
			err.Code = code
		}
	}
	if err.Message == "" || err.Message == "app invoke failed" || strings.HasPrefix(err.Message, "app invoke failed with status ") {
		if message, ok := object["message"].(string); ok && strings.TrimSpace(message) != "" {
			err.Message = message
		}
	}
	if err.Code == "" {
		if code, ok := object["code"].(string); ok && strings.TrimSpace(code) != "" {
			err.Code = code
		}
	}
}

func graphqlErrorMessage(errors []any) string {
	if len(errors) == 0 {
		return "GraphQL returned errors"
	}
	if object, ok := errors[0].(map[string]any); ok {
		if message, ok := object["message"].(string); ok && strings.TrimSpace(message) != "" {
			return message
		}
	}
	return "GraphQL returned errors"
}
