package core

import "context"

const BearerScheme = "Bearer "

type AuthProvider interface {
	Name() string
	LoginURL(state string) (string, error)
	HandleCallback(ctx context.Context, code string) (*UserIdentity, error)
	ValidateToken(ctx context.Context, token string) (*UserIdentity, error)
}
