package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// AuthenticatedUser is the authenticated principal returned by an auth
// provider.
type AuthenticatedUser = proto.AuthenticatedUser

// BeginLoginRequest starts an interactive provider login flow.
type BeginLoginRequest = proto.BeginLoginRequest

// BeginLoginResponse contains the provider-managed authorization URL and
// opaque state.
type BeginLoginResponse = proto.BeginLoginResponse

// CompleteLoginRequest finishes an interactive login flow.
type CompleteLoginRequest = proto.CompleteLoginRequest

// AuthProvider serves the Gestalt authentication protocol.
type AuthProvider interface {
	PluginProvider
	BeginLogin(ctx context.Context, req *BeginLoginRequest) (*BeginLoginResponse, error)
	CompleteLogin(ctx context.Context, req *CompleteLoginRequest) (*AuthenticatedUser, error)
}

// ExternalTokenValidator is implemented by auth providers that can validate
// tokens minted outside the interactive login flow.
type ExternalTokenValidator interface {
	ValidateExternalToken(ctx context.Context, token string) (*AuthenticatedUser, error)
}

// SessionTTLProvider is implemented by auth providers that want the host to
// persist sessions for a fixed amount of time.
type SessionTTLProvider interface {
	SessionTTL() time.Duration
}
