package core

import "context"

type AuthProvider interface {
	Name() string
	LoginURL(state string) string
	HandleCallback(ctx context.Context, code string) (*UserIdentity, error)
	ValidateToken(ctx context.Context, token string) (*UserIdentity, error)
}
