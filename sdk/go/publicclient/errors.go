package publicclient

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
)

const responseKindHeader = "X-Gestalt-Response-Kind"

const responseKindOperationResult = "operation-result"

// GatewayError is a grpc-gateway style error response from the public REST API.
type GatewayError struct {
	Code    int32
	Message string
	Details []any
	Status  int
	Body    []byte
}

func (e *GatewayError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("gestalt gateway error (status %d)", e.Status)
}

// IsOperationResult reports whether headers mark an OperationResult passthrough.
func IsOperationResult(headers http.Header) bool {
	return headers.Get(responseKindHeader) == responseKindOperationResult
}

// DecodeGatewayError parses a non-operation-result REST error body when possible.
func DecodeGatewayError(status int, body []byte) error {
	code := httpStatusToGestaltCode(status)
	message := fmt.Sprintf("request failed with status %d", status)
	if len(body) > 0 {
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil {
			if text, ok := payload["message"].(string); ok && text != "" {
				message = text
			}
			if nested, ok := payload["error"].(map[string]any); ok {
				if text, ok := nested["message"].(string); ok && text != "" {
					message = text
				}
				if rawCode, ok := nested["code"].(string); ok && rawCode != "" {
					code = gestaltCodeFromString(rawCode)
				}
			}
			if numeric, ok := payload["code"].(float64); ok {
				code = generated.GestaltErrorCode(int32(numeric))
			}
		}
	}
	return &generated.GestaltError{Code: code, Message: message}
}

func httpStatusToGestaltCode(status int) generated.GestaltErrorCode {
	switch status {
	case http.StatusBadRequest:
		return generated.GestaltErrorCodeInvalidArgument
	case http.StatusUnauthorized:
		return generated.GestaltErrorCodeUnauthenticated
	case http.StatusForbidden:
		return generated.GestaltErrorCodePermissionDenied
	case http.StatusNotFound:
		return generated.GestaltErrorCodeNotFound
	case http.StatusConflict:
		return generated.GestaltErrorCodeAlreadyExists
	case http.StatusPreconditionFailed:
		return generated.GestaltErrorCodeFailedPrecondition
	case 429:
		return generated.GestaltErrorCodeResourceExhausted
	case 499:
		return generated.GestaltErrorCodeCanceled
	case http.StatusInternalServerError:
		return generated.GestaltErrorCodeInternal
	case http.StatusNotImplemented:
		return generated.GestaltErrorCodeUnimplemented
	case http.StatusServiceUnavailable:
		return generated.GestaltErrorCodeUnavailable
	case http.StatusGatewayTimeout:
		return generated.GestaltErrorCodeDeadlineExceeded
	default:
		return generated.GestaltErrorCodeUnknown
	}
}

func gestaltCodeFromString(raw string) generated.GestaltErrorCode {
	switch raw {
	case "CANCELED", "canceled":
		return generated.GestaltErrorCodeCanceled
	case "INVALID_ARGUMENT", "invalid_argument":
		return generated.GestaltErrorCodeInvalidArgument
	case "DEADLINE_EXCEEDED", "deadline_exceeded":
		return generated.GestaltErrorCodeDeadlineExceeded
	case "NOT_FOUND", "not_found":
		return generated.GestaltErrorCodeNotFound
	case "ALREADY_EXISTS", "already_exists":
		return generated.GestaltErrorCodeAlreadyExists
	case "PERMISSION_DENIED", "permission_denied":
		return generated.GestaltErrorCodePermissionDenied
	case "RESOURCE_EXHAUSTED", "resource_exhausted":
		return generated.GestaltErrorCodeResourceExhausted
	case "FAILED_PRECONDITION", "failed_precondition":
		return generated.GestaltErrorCodeFailedPrecondition
	case "ABORTED", "aborted":
		return generated.GestaltErrorCodeAborted
	case "OUT_OF_RANGE", "out_of_range":
		return generated.GestaltErrorCodeOutOfRange
	case "UNIMPLEMENTED", "unimplemented":
		return generated.GestaltErrorCodeUnimplemented
	case "INTERNAL", "internal":
		return generated.GestaltErrorCodeInternal
	case "UNAVAILABLE", "unavailable":
		return generated.GestaltErrorCodeUnavailable
	case "DATA_LOSS", "data_loss":
		return generated.GestaltErrorCodeDataLoss
	case "UNAUTHENTICATED", "unauthenticated":
		return generated.GestaltErrorCodeUnauthenticated
	default:
		return generated.GestaltErrorCodeUnknown
	}
}
