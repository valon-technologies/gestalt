package core

import "context"

const BearerScheme = "Bearer "

type AuthenticationProvider interface {
	Name() string
	LoginURL(state string) (string, error)
	HandleCallback(ctx context.Context, code string) (*UserIdentity, error)
	ValidateToken(ctx context.Context, token string) (*UserIdentity, error)
}

// LoginURLContextProvider is an optional extension that lets authentication
// providers participate in request-scoped cancellation and tracing during login
// start.
type LoginURLContextProvider interface {
	LoginURLContext(ctx context.Context, state string) (string, error)
}
