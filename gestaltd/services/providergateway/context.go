package providergateway

import "context"

type contextKey string

const (
	sourceContextKey         contextKey = "provider_gateway_source"
	requestContextContextKey contextKey = "provider_gateway_request_context"
	callerTokenContextKey    contextKey = "provider_gateway_caller_token"
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

func WithCallerToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, callerTokenContextKey, token)
}

func CallerTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(callerTokenContextKey).(string)
	return token
}
