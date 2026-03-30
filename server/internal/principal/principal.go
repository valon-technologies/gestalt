package principal

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
)

type Source int

const (
	SourceSession Source = iota
	SourceAPIToken
	SourceEnv
)

const IdentityPrincipal = "__identity__"

type Principal struct {
	Identity *core.UserIdentity
	UserID   string
	Source   Source
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(contextKey{}).(*Principal)
	return p
}
