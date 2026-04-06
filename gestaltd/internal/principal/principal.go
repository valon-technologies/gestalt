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
	Scopes   []string
}

func (s Source) String() string {
	switch s {
	case SourceSession:
		return "session"
	case SourceAPIToken:
		return "api_token"
	case SourceEnv:
		return "env"
	default:
		return ""
	}
}

func (p *Principal) AuthSource() string {
	if p == nil {
		return ""
	}
	if p.Identity == nil && p.UserID == "" && len(p.Scopes) == 0 {
		return ""
	}
	return p.Source.String()
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(contextKey{}).(*Principal)
	return p
}
