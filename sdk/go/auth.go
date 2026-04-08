package gestalt

import (
	"context"
	"time"
)

// AuthProvider is the runtime contract for platform-auth providers. The host
// owns Gestalt session issuance; auth providers return authenticated identity
// only.
type AuthProvider interface {
	RuntimeProvider
	BeginLogin(ctx context.Context, req BeginLoginRequest) (*BeginLoginResponse, error)
	CompleteLogin(ctx context.Context, req CompleteLoginRequest) (*AuthenticatedUser, error)
}

// ExternalTokenValidator is implemented by auth providers that can validate a
// bearer token issued by the upstream identity provider.
type ExternalTokenValidator interface {
	ValidateExternalToken(ctx context.Context, token string) (*AuthenticatedUser, error)
}

// SessionTTLProvider is implemented by auth providers that want the host to use
// a non-default Gestalt session lifetime.
type SessionTTLProvider interface {
	SessionTTL() time.Duration
}

type BeginLoginRequest struct {
	CallbackURL string
	HostState   string
	Scopes      []string
	Options     map[string]string
}

type BeginLoginResponse struct {
	AuthorizationURL string
	ProviderState    []byte
}

type CompleteLoginRequest struct {
	Query         map[string]string
	ProviderState []byte
	CallbackURL   string
}

type AuthenticatedUser struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
	Claims        map[string]string
}
