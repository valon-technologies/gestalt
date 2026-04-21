package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

// AuthenticatedUser is the authenticated principal returned by an
// authentication provider.
type AuthenticatedUser = proto.AuthenticatedUser

// BeginAuthenticationRequest starts an interactive provider authentication flow.
type BeginAuthenticationRequest = proto.BeginAuthenticationRequest

// BeginAuthenticationResponse contains the provider-managed authorization URL and
// opaque state.
type BeginAuthenticationResponse = proto.BeginAuthenticationResponse

// CompleteAuthenticationRequest finishes an interactive authentication flow.
type CompleteAuthenticationRequest = proto.CompleteAuthenticationRequest

// AuthenticateRequest asks the provider to validate a bearer token or
// provider-specific HTTP authentication input.
type AuthenticateRequest = proto.AuthenticateRequest

// TokenAuthInput carries a bearer token for external authentication.
type TokenAuthInput = proto.TokenAuthInput

// HTTPRequestAuthInput carries an HTTP request that the provider should
// authenticate.
type HTTPRequestAuthInput = proto.HTTPRequestAuthInput

// AuthenticationProvider serves the Gestalt interactive authentication
// protocol.
type AuthenticationProvider interface {
	PluginProvider
	BeginAuthentication(ctx context.Context, req *BeginAuthenticationRequest) (*BeginAuthenticationResponse, error)
	CompleteAuthentication(ctx context.Context, req *CompleteAuthenticationRequest) (*AuthenticatedUser, error)
}

// LegacyAuthenticationProvider serves the deprecated login/callback contract.
// New providers should implement AuthenticationProvider instead.
type LegacyAuthenticationProvider interface {
	PluginProvider
	BeginLogin(ctx context.Context, req *BeginLoginRequest) (*BeginLoginResponse, error)
	CompleteLogin(ctx context.Context, req *CompleteLoginRequest) (*AuthenticatedUser, error)
}

// BeginLoginRequest starts an interactive provider login flow.
type BeginLoginRequest = proto.BeginLoginRequest

// BeginLoginResponse contains the provider-managed authorization URL and
// opaque state.
type BeginLoginResponse = proto.BeginLoginResponse

// CompleteLoginRequest finishes an interactive login flow.
type CompleteLoginRequest = proto.CompleteLoginRequest

// Authenticator is implemented by authentication providers that can validate
// externally minted bearer tokens or signed HTTP requests.
type Authenticator interface {
	Authenticate(ctx context.Context, req *AuthenticateRequest) (*AuthenticatedUser, error)
}

// ExternalTokenValidator is implemented by authentication providers that can
// validate bearer tokens minted outside the interactive login flow. New
// providers should implement Authenticator instead.
type ExternalTokenValidator interface {
	ValidateExternalToken(ctx context.Context, token string) (*AuthenticatedUser, error)
}

// SessionTTLProvider is implemented by authentication providers that want the
// host to persist sessions for a fixed amount of time.
type SessionTTLProvider interface {
	SessionTTL() time.Duration
}
