package invocation

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RemoteInvokeError maps a remote public gRPC status to a local invocation error.
func RemoteInvokeError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.Unauthenticated:
		return ErrNotAuthenticated
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", ErrAuthorizationDenied, strings.TrimSpace(st.Message()))
	case codes.NotFound:
		msg := strings.ToLower(strings.TrimSpace(st.Message()))
		if strings.Contains(msg, "operation") {
			return ErrOperationNotFound
		}
		return ErrProviderNotFound
	case codes.FailedPrecondition:
		msg := strings.TrimSpace(st.Message())
		switch {
		case strings.Contains(msg, ErrNoCredential.Error()):
			return fmt.Errorf("%w: %s", ErrNoCredential, msg)
		case strings.Contains(msg, ErrReconnectRequired.Error()):
			return fmt.Errorf("%w: %s", ErrReconnectRequired, msg)
		default:
			return err
		}
	default:
		return err
	}
}
