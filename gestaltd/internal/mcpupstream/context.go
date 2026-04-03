package mcpupstream

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

type contextKey struct{}

type metaKey struct{}

func WithUpstreamToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, contextKey{}, token)
}

func UpstreamTokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

func WithCallToolMeta(ctx context.Context, meta *mcpgo.Meta) context.Context {
	if meta == nil {
		return ctx
	}
	return context.WithValue(ctx, metaKey{}, meta)
}

func CallToolMetaFromContext(ctx context.Context) *mcpgo.Meta {
	v, _ := ctx.Value(metaKey{}).(*mcpgo.Meta)
	return v
}
