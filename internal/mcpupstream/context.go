package mcpupstream

import "context"

type contextKey struct{}

func WithUpstreamToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, contextKey{}, token)
}

func UpstreamTokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}
