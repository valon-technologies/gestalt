package plugininvoker

import "context"

type invocationTokenCtxKey struct{}

func WithInvocationToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, invocationTokenCtxKey{}, token)
}

func InvocationTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(invocationTokenCtxKey{}).(string)
	return token
}
