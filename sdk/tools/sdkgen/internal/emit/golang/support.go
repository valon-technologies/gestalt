package golang

import _ "embed"

// supportFile is the shared public file: the canonical error model. It is
// emitted once as rpc_support.go.
//
//go:embed rpc_support.go.tmpl
var supportFile string

// codecSupportFile is the shared codec runtime: the nil-safe wire converters
// for the well-known types used by the generated codec files. It is emitted
// once as support_codec.go.
//
//go:embed support_codec.go.tmpl
var codecSupportFile string

// invokeSupportFile is the JSON operation-envelope decode runtime, emitted as
// support_invoke.go when any method carries the json_result annotation.
const invokeSupportFile = `package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// InvokeError is the canonical app invocation payload error: an HTTP-error
// status, an error envelope, or an undecodable result body. Transport
// failures stay *GestaltError; both arrive through the error return of
// json_result methods.
type InvokeError struct {
	App       string
	Operation string
	Status    int32
	Code      string
	Message   string
	Body      any
	RawBody   []byte
}

func (e *InvokeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	if e.Status > 0 {
		return fmt.Sprintf("app invoke failed with status %d", e.Status)
	}
	return "app invoke failed"
}

// DecodeAppResult decodes one app operation result with the standard JSON
// envelope semantics: success envelopes return their data, error envelopes
// and HTTP-error statuses return *InvokeError, and any other JSON body passes
// through unchanged.
func DecodeAppResult(app, operation string, status int32, body []byte) (any, error) {
	parsed, err := parseJSONResultBody(body)
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
			applyInvokeErrorFields(invokeErr, parsed)
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
				applyInvokeErrorFields(invokeErr, parsed)
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

// DecodeGraphQLResult decodes one GraphQL invocation result like
// DecodeAppResult and additionally returns *InvokeError when the response
// carries a non-empty GraphQL errors array.
func DecodeGraphQLResult(app string, status int32, body []byte) (any, error) {
	decoded, err := DecodeAppResult(app, "graphql", status, body)
	if err != nil {
		return nil, err
	}
	if raw, rawErr := parseJSONResultBody(body); rawErr == nil {
		if err := graphQLErrors(app, body, raw); err != nil {
			return nil, err
		}
	}
	if err := graphQLErrors(app, body, decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// JSONResultAs decodes the value returned by a json_result method into T,
// passing any invocation error through unchanged.
func JSONResultAs[T any](value any, err error) (T, error) {
	var out T
	if err != nil {
		return out, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func parseJSONResultBody(body []byte) (any, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func graphQLErrors(app string, rawBody []byte, value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	errorsValue, ok := object["errors"].([]any)
	if !ok || len(errorsValue) == 0 {
		return nil
	}
	message := "GraphQL returned errors"
	if first, ok := errorsValue[0].(map[string]any); ok {
		if text, ok := first["message"].(string); ok && strings.TrimSpace(text) != "" {
			message = text
		}
	}
	return &InvokeError{
		App:       app,
		Operation: "graphql",
		Code:      "graphql_errors",
		Message:   message,
		Body:      value,
		RawBody:   rawBody,
	}
}

func applyInvokeErrorFields(err *InvokeError, parsed any) {
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
`

// contextSupportFile is the client option machinery, emitted as
// support_context.go when any service carries a request context. The fmt verb
// is the native request context type.
const contextSupportFile = `package client

// ClientOption configures a generated client constructor.
type ClientOption func(*clientOptions)

type clientOptions struct {
	requestContext %[1]s
}

// WithRequestContext sets the client's default request context: outgoing
// requests whose context field is unset carry it.
func WithRequestContext(context %[1]s) ClientOption {
	return func(o *clientOptions) { o.requestContext = context }
}

func applyClientOptions(opts []ClientOption) clientOptions {
	var options clientOptions
	for _, opt := range opts {
		opt(&options)
	}
	return options
}
`
