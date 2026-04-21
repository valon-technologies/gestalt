package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// AuthenticatedUser is the authenticated principal returned by an
// authentication provider.
type AuthenticatedUser = proto.AuthenticatedUser

// BeginLoginRequest starts an interactive provider login flow.
type BeginLoginRequest = proto.BeginLoginRequest

// BeginLoginResponse contains the provider-managed authorization URL and
// opaque state.
type BeginLoginResponse = proto.BeginLoginResponse

// CompleteLoginRequest finishes an interactive login flow.
type CompleteLoginRequest = proto.CompleteLoginRequest

// AuthenticationProvider serves the Gestalt authentication protocol.
type AuthenticationProvider interface {
	PluginProvider
	BeginLogin(ctx context.Context, req *BeginLoginRequest) (*BeginLoginResponse, error)
	CompleteLogin(ctx context.Context, req *CompleteLoginRequest) (*AuthenticatedUser, error)
}

// ExternalTokenValidator is implemented by authentication providers that can
// validate tokens minted outside the interactive login flow.
type ExternalTokenValidator interface {
	ValidateExternalToken(ctx context.Context, token string) (*AuthenticatedUser, error)
}

// SessionTTLProvider is implemented by authentication providers that want the
// host to persist sessions for a fixed amount of time.
type SessionTTLProvider interface {
	SessionTTL() time.Duration
}
