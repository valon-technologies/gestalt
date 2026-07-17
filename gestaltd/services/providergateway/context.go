package providergateway

import "context"

type contextKey string

const requestContextContextKey contextKey = "provider_gateway_request_context"

func WithRequestContext(ctx context.Context, reqCtx *RequestContext) context.Context {
	if reqCtx == nil {
		return ctx
	}
	return context.WithValue(ctx, requestContextContextKey, reqCtx)
}

func RequestContextFromContext(ctx context.Context) *RequestContext {
	reqCtx, _ := ctx.Value(requestContextContextKey).(*RequestContext)
	return reqCtx
}
