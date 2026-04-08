package gestalt

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
