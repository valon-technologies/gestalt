package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type AuthenticatedUser = proto.AuthenticatedUser
type BeginLoginRequest = proto.BeginLoginRequest
type BeginLoginResponse = proto.BeginLoginResponse
type CompleteLoginRequest = proto.CompleteLoginRequest

type AuthProvider interface {
	PluginProvider
	BeginLogin(ctx context.Context, req *BeginLoginRequest) (*BeginLoginResponse, error)
	CompleteLogin(ctx context.Context, req *CompleteLoginRequest) (*AuthenticatedUser, error)
}

type ExternalTokenValidator interface {
	ValidateExternalToken(ctx context.Context, token string) (*AuthenticatedUser, error)
}

type SessionTTLProvider interface {
	SessionTTL() time.Duration
}
