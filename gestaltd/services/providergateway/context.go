package providergateway

import "context"

type contextKey string

const (
	sourceContextKey         contextKey = "provider_gateway_source"
	requestContextContextKey contextKey = "provider_gateway_request_context"
)

func WithSource(ctx context.Context, source GatewaySource) context.Context {
	if source == "" {
		return ctx
	}
	return context.WithValue(ctx, sourceContextKey, source)
}

func SourceFromContext(ctx context.Context) GatewaySource {
	source, _ := ctx.Value(sourceContextKey).(GatewaySource)
	if source == "" {
		return GatewaySourceInternal
	}
	return source
}

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
