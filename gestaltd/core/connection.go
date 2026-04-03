package core

import "context"

type connectionParamsKey struct{}

func WithConnectionParams(ctx context.Context, params map[string]string) context.Context {
	return context.WithValue(ctx, connectionParamsKey{}, params)
}

func ConnectionParams(ctx context.Context) map[string]string {
	params, _ := ctx.Value(connectionParamsKey{}).(map[string]string)
	return params
}
