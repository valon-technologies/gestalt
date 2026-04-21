package core

import "context"

const BearerScheme = "Bearer "

type BeginAuthenticationRequest struct {
	CallbackURL string
	HostState   string
	Scopes      []string
	Options     map[string]string
}

type BeginAuthenticationResponse struct {
	AuthorizationURL string
	ProviderState    []byte
}

type CompleteAuthenticationRequest struct {
	CallbackURL   string
	Query         map[string]string
	ProviderState []byte
}

type TokenAuthInput struct {
	Token string
}

type HTTPRequestAuthInput struct {
	Method  string
	URL     string
	Headers map[string]string
	Query   map[string]string
}

type AuthenticateRequest struct {
	Token   *TokenAuthInput
	HTTP    *HTTPRequestAuthInput
	Options map[string]string
}

type AuthenticationProvider interface {
	Name() string
	BeginAuthentication(ctx context.Context, req *BeginAuthenticationRequest) (*BeginAuthenticationResponse, error)
	CompleteAuthentication(ctx context.Context, req *CompleteAuthenticationRequest) (*UserIdentity, error)
}

// Authenticator is an optional extension that lets authentication providers
// validate externally minted tokens or provider-specific request signatures.
// Host-issued Gestalt session tokens remain a host concern.
type Authenticator interface {
	Authenticate(ctx context.Context, req *AuthenticateRequest) (*UserIdentity, error)
}
