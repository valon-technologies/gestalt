package plugins

import (
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func providerExecuteError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, invocation.ErrNoCredential), errors.Is(err, invocation.ErrReconnectRequired):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Errorf(codes.Unknown, "execute: %v", err)
	}
}

func RemoteProviderExecuteError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.Unauthenticated:
		return invocation.ErrNotAuthenticated
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", invocation.ErrAuthorizationDenied, strings.TrimSpace(st.Message()))
	case codes.NotFound:
		msg := strings.ToLower(strings.TrimSpace(st.Message()))
		if strings.Contains(msg, "operation") {
			return invocation.ErrOperationNotFound
		}
		return invocation.ErrProviderNotFound
	case codes.FailedPrecondition:
		message := strings.TrimSpace(st.Message())
		switch {
		case strings.Contains(message, invocation.ErrNoCredential.Error()):
			return fmt.Errorf("%w: %s", invocation.ErrNoCredential, message)
		case strings.Contains(message, invocation.ErrReconnectRequired.Error()):
			return fmt.Errorf("%w: %s", invocation.ErrReconnectRequired, message)
		default:
			return err
		}
	default:
		return err
	}
}
