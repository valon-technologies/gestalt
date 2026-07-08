package remote

import (
	"errors"
	"fmt"

	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func invokeError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s", invocation.ErrNotAuthenticated, status.Convert(err).Message())
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, status.Convert(err).Message())
	case codes.NotFound:
		return fmt.Errorf("%w: %s", invocation.ErrProviderNotFound, status.Convert(err).Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", invocation.ErrInvalidInvocation, status.Convert(err).Message())
	case codes.FailedPrecondition:
		msg := status.Convert(err).Message()
		switch {
		case errors.Is(err, invocation.ErrNoCredential), containsCredentialMessage(msg, invocation.ErrNoCredential):
			return fmt.Errorf("%w: %s", invocation.ErrNoCredential, msg)
		case errors.Is(err, invocation.ErrReconnectRequired), containsCredentialMessage(msg, invocation.ErrReconnectRequired):
			return fmt.Errorf("%w: %s", invocation.ErrReconnectRequired, msg)
		default:
			return err
		}
	default:
		return err
	}
}

func containsCredentialMessage(msg string, target error) bool {
	if target == nil {
		return false
	}
	return msg != "" && (msg == target.Error() || containsSubstring(msg, target.Error()))
}

func containsSubstring(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) && indexSubstring(s, substr) >= 0
}

func indexSubstring(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
