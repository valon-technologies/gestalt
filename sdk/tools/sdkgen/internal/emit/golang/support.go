package golang

import (
	_ "embed"
	"strings"
)

// supportFile is the shared error model template. Provider clients keep the
// gRPC and stream extensions.
//
//go:embed rpc_support.go.tmpl
var supportFile string

// codecSupportFile is the shared codec runtime emitted as support_codec.go.
//
//go:embed support_codec.go.tmpl
var codecSupportFile string

const publicRPCSupportFile = `package generated

import gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"

type (
	GestaltError     = gestaltclient.GestaltError
	GestaltErrorCode = gestaltclient.GestaltErrorCode
	RpcStatus        = gestaltclient.RpcStatus
)

func toGestaltError(err error) *GestaltError {
	return gestaltclient.ToGestaltError(err)
}
`

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
//
// Envelope error.code on the wire maps to Reason.
type InvokeError struct {
	GestaltError
	App       string
	Operation string
	Status    int32
	Reason    string
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
	if e.Reason != "" {
		return e.Reason
	}
	if e.Status > 0 {
		return fmt.Sprintf("app invoke failed with status %d", e.Status)
	}
	return "app invoke failed"
}

// Unwrap exposes the embedded GestaltError for errors.As and errors.Is.
func (e *InvokeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return &e.GestaltError
}

// IsOK reports whether status is an HTTP success status (200-299).
func IsOK(status int32) bool {
	return status >= 200 && status <= 299
}

// RequireOK returns nil when status is an HTTP success status (200-299) and
// the *InvokeError DecodeAppResult builds for an HTTP-error status otherwise.
func RequireOK(app, operation string, status int32, body []byte) error {
	if IsOK(status) {
		return nil
	}
	parsed, err := parseJSONResultBody(body)
	return statusInvokeError(app, operation, status, body, parsed, err)
}

func newInvokeError(app, operation string, status int32, message, reason string, body any, rawBody []byte) *InvokeError {
	code := GestaltErrorCodeUnknown
	if status > 0 {
		code = HTTPStatusToGestaltCode(status)
	} else if reason == "graphql_errors" || message == "app invoke response is not valid JSON" || message == "operation result body is not valid JSON" {
		code = GestaltErrorCodeInternal
	}
	return &InvokeError{
		GestaltError: GestaltError{
			Code:    code,
			Message: message,
		},
		App:       app,
		Operation: operation,
		Status:    status,
		Reason:    reason,
		Body:      body,
		RawBody:   rawBody,
	}
}

// statusInvokeError builds the *InvokeError for an HTTP-error status: the raw
// body always rides along, and a JSON body additionally carries its parsed
// form and error envelope fields.
func statusInvokeError(app, operation string, status int32, body []byte, parsed any, parseErr error) *InvokeError {
	invokeErr := newInvokeError(
		app,
		operation,
		status,
		fmt.Sprintf("app invoke failed with status %d", status),
		"",
		nil,
		body,
	)
	if parseErr == nil {
		invokeErr.Body = parsed
		message, reason := extractInvokeErrorFields(parsed)
		if message != "" {
			invokeErr.Message = message
		}
		if reason != "" {
			invokeErr.Reason = reason
		}
	}
	return invokeErr
}

// DecodeAppResult decodes one app operation result with the standard JSON
// envelope semantics: success envelopes return their data, error envelopes
// and HTTP-error statuses return *InvokeError, and any other JSON body passes
// through unchanged.
func DecodeAppResult(app, operation string, status int32, body []byte) (any, error) {
	parsed, err := parseJSONResultBody(body)
	if !IsOK(status) {
		return nil, statusInvokeError(app, operation, status, body, parsed, err)
	}
	if err != nil {
		return nil, newInvokeError(
			app,
			operation,
			0,
			"app invoke response is not valid JSON",
			"",
			nil,
			body,
		)
	}
	if object, ok := parsed.(map[string]any); ok {
		if statusValue, ok := object["status"].(string); ok {
			switch statusValue {
			case "error":
				message, reason := extractInvokeErrorFields(parsed)
				if message == "" {
					message = "app invoke failed"
				}
				return nil, newInvokeError(app, operation, 0, message, reason, parsed, body)
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
	return newInvokeError(app, "graphql", 0, message, "graphql_errors", value, rawBody)
}

func extractInvokeErrorFields(parsed any) (message, reason string) {
	object, ok := parsed.(map[string]any)
	if !ok {
		return "", ""
	}
	if nested, ok := object["error"].(map[string]any); ok {
		if text, ok := nested["message"].(string); ok && strings.TrimSpace(text) != "" {
			message = text
		}
		if code, ok := nested["code"].(string); ok && strings.TrimSpace(code) != "" {
			reason = code
		}
	}
	if message == "" {
		if text, ok := object["message"].(string); ok && strings.TrimSpace(text) != "" {
			message = text
		}
	}
	if reason == "" {
		if code, ok := object["code"].(string); ok && strings.TrimSpace(code) != "" {
			reason = code
		}
	}
	return message, reason
}
`

// contextSupportFile is the client option machinery, emitted as
// support_context.go when any service carries a request context.
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

var publicCodecSupportFile = strings.Replace(codecSupportFile, "package client", "package generated", 1)
