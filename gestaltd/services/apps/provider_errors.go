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

func remoteProviderExecuteError(err error) error {
	if status.Code(err) != codes.FailedPrecondition {
		return err
	}
	message := status.Convert(err).Message()
	switch {
	case strings.Contains(message, invocation.ErrNoCredential.Error()):
		return fmt.Errorf("%w: %s", invocation.ErrNoCredential, message)
	case strings.Contains(message, invocation.ErrReconnectRequired.Error()):
		return fmt.Errorf("%w: %s", invocation.ErrReconnectRequired, message)
	default:
		return err
	}
}
