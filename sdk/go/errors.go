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
	internalErrorBodyFallback = `{"error":"internal error"}`
)

type operationError struct {
	status  int
	message string
	cause   error
}

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
	message = stringOr(message, http.StatusText(status))
	return &operationError{
		status:  status,
		message: message,
		cause:   cause,
	}
}

func operationResultFromError(err error) *OperationResult {
	if err == nil {
		return nil
	}
	status := http.StatusInternalServerError
	message := err.Error()
	var opErr *operationError
	if errors.As(err, &opErr) {
		if opErr.status != 0 {
			status = opErr.status
		}
		message = stringOr(opErr.message, opErr.Error())
	}
	return operationResult(status, message)
}

func recoveredOperationResult(operation string, recovered any) *OperationResult {
	message := fmt.Sprint(recovered)
	if operation == "" {
		fmt.Fprintf(os.Stderr, "panic in Gestalt operation: %v\n", recovered)
	} else {
		fmt.Fprintf(os.Stderr, "panic in Gestalt operation %q: %v\n", operation, recovered)
	}
	_, _ = os.Stderr.Write(debug.Stack())
	return operationResult(http.StatusInternalServerError, message)
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

func stringOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

var (
	ErrExternalTokenValidationUnsupported = errors.New("auth provider does not support external token validation")
	ErrOAuthRegistrationStoreUnsupported  = errors.New("datastore provider does not support oauth registrations")
)

func providerRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrExternalTokenValidationUnsupported),
		errors.Is(err, ErrOAuthRegistrationStoreUnsupported):
		return status.Error(codes.Unimplemented, err.Error())
	}
	if st, ok := status.FromError(err); ok {
		return st.Err()
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}
