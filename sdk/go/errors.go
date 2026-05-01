package gestalt

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	operationErrorField       = "error"
	unknownOperationMessage   = "unknown operation"
	routerNilMessage          = "router is nil"
	nilResultMessage          = "provider returned nil result"
	internalErrorMessage      = "internal error"
	internalErrorBodyFallback = `{"error":"internal error"}`
)

type operationError struct {
	status  int
	message string
	cause   error
}

// StatusCode is a transport-independent provider error category.
type StatusCode string

const (
	CodeCanceled           StatusCode = "canceled"
	CodeUnknown            StatusCode = "unknown"
	CodeInvalidArgument    StatusCode = "invalid_argument"
	CodeNotFound           StatusCode = "not_found"
	CodeAlreadyExists      StatusCode = "already_exists"
	CodeFailedPrecondition StatusCode = "failed_precondition"
	CodeOutOfRange         StatusCode = "out_of_range"
	CodeUnauthenticated    StatusCode = "unauthenticated"
	CodePermissionDenied   StatusCode = "permission_denied"
	CodeUnimplemented      StatusCode = "unimplemented"
	CodeInternal           StatusCode = "internal"
)

type statusError struct {
	code    StatusCode
	message string
}

func (e statusError) Error() string { return e.message }

// StatusError returns an error with a provider status code.
func StatusError(code StatusCode, message string) error {
	return statusError{code: code, message: message}
}

func Canceled(message string) error           { return StatusError(CodeCanceled, message) }
func InvalidArgument(message string) error    { return StatusError(CodeInvalidArgument, message) }
func NotFound(message string) error           { return StatusError(CodeNotFound, message) }
func AlreadyExists(message string) error      { return StatusError(CodeAlreadyExists, message) }
func FailedPrecondition(message string) error { return StatusError(CodeFailedPrecondition, message) }
func OutOfRange(message string) error         { return StatusError(CodeOutOfRange, message) }
func Unauthenticated(message string) error    { return StatusError(CodeUnauthenticated, message) }
func PermissionDenied(message string) error   { return StatusError(CodePermissionDenied, message) }
func Unimplemented(message string) error      { return StatusError(CodeUnimplemented, message) }
func Internal(message string) error           { return StatusError(CodeInternal, message) }

func (e *operationError) Error() string {
	if e.cause != nil {
		return e.cause.Error()
	}
	return e.message
}

func (e *operationError) Unwrap() error {
	return e.cause
}

func newOperationError(status int, message string, cause error) error {
	if message == "" {
		message = http.StatusText(status)
	}
	return &operationError{
		status:  status,
		message: message,
		cause:   cause,
	}
}

// Error returns an operation error that causes the handler response to use the
// provided HTTP status and message.
func Error(status int, message string) error {
	return newOperationError(status, message, nil)
}

func operationResultFromError(err error) *OperationResult {
	if err == nil {
		return nil
	}
	status := http.StatusInternalServerError
	message := internalErrorMessage
	var opErr *operationError
	if errors.As(err, &opErr) {
		if opErr.status != 0 {
			status = opErr.status
		}
		message = opErr.message
		if message == "" {
			message = opErr.Error()
		}
	} else {
		fmt.Fprintf(os.Stderr, "internal error in Gestalt operation: %v\n", err)
	}
	return operationResult(status, message)
}

func recoveredOperationResult(operation string, recovered any) *OperationResult {
	if operation == "" {
		fmt.Fprintf(os.Stderr, "panic in Gestalt operation: %v\n", recovered)
	} else {
		fmt.Fprintf(os.Stderr, "panic in Gestalt operation %q: %v\n", operation, recovered)
	}
	_, _ = os.Stderr.Write(debug.Stack())
	return operationResult(http.StatusInternalServerError, internalErrorMessage)
}

func protectedOperationResult(operation string, fn func() (*OperationResult, error)) (result *OperationResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = recoveredOperationResult(operation, recovered)
		}
	}()
	result, err := fn()
	if err != nil {
		return operationResultFromError(err)
	}
	return result
}

func operationResult(status int, message string) *OperationResult {
	return &OperationResult{
		Status: status,
		Body:   operationErrorBody(message),
	}
}

func operationErrorBody(message string) string {
	data, err := json.Marshal(map[string]string{operationErrorField: message})
	if err != nil {
		return internalErrorBodyFallback
	}
	return string(data)
}

var (
	// ErrSecretNotFound indicates that a named secret does not exist.
	ErrSecretNotFound = errors.New("secret not found")
	// ErrExternalCredentialNotFound indicates that the requested external
	// credential does not exist.
	ErrExternalCredentialNotFound = errors.New("external credential not found")
	// ErrExternalTokenValidationUnsupported indicates that the authentication provider
	// does not implement external token validation.
	ErrExternalTokenValidationUnsupported = errors.New("authentication provider does not support external token validation")
	// ErrOAuthRegistrationStoreUnsupported indicates that the datastore provider
	// does not implement OAuth registration storage.
	ErrOAuthRegistrationStoreUnsupported = errors.New("datastore provider does not support oauth registrations")
)

func providerRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var coded statusError
	if errors.As(err, &coded) {
		return status.Error(grpcCode(coded.code), coded.message)
	}
	switch {
	case errors.Is(err, ErrSecretNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrExternalCredentialNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrExternalTokenValidationUnsupported),
		errors.Is(err, ErrOAuthRegistrationStoreUnsupported):
		return status.Error(codes.Unimplemented, err.Error())
	}
	if st, ok := status.FromError(err); ok {
		return st.Err()
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}

func grpcCode(code StatusCode) codes.Code {
	switch code {
	case CodeCanceled:
		return codes.Canceled
	case CodeInvalidArgument:
		return codes.InvalidArgument
	case CodeNotFound:
		return codes.NotFound
	case CodeAlreadyExists:
		return codes.AlreadyExists
	case CodeFailedPrecondition:
		return codes.FailedPrecondition
	case CodeOutOfRange:
		return codes.OutOfRange
	case CodeUnauthenticated:
		return codes.Unauthenticated
	case CodePermissionDenied:
		return codes.PermissionDenied
	case CodeUnimplemented:
		return codes.Unimplemented
	case CodeInternal:
		return codes.Internal
	default:
		return codes.Unknown
	}
}
