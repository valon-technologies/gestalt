package remote

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatusError maps a remote public gRPC status to a local provider error.
func StatusError(err error) error {
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
		return invocation.ErrProviderNotFound
	default:
		return err
	}
}
