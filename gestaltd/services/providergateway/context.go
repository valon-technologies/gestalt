package providergateway

import "context"

type contextKey string

const (
	sourceContextKey            contextKey = "provider_gateway_source"
	invokingSubjectIDContextKey contextKey = "provider_gateway_invoking_subject_id"
	requestContextContextKey    contextKey = "provider_gateway_request_context"
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

func WithInvokingSubjectID(ctx context.Context, subjectID string) context.Context {
	if subjectID == "" {
		return ctx
	}
	return context.WithValue(ctx, invokingSubjectIDContextKey, subjectID)
}

func InvokingSubjectIDFromContext(ctx context.Context) string {
	subjectID, _ := ctx.Value(invokingSubjectIDContextKey).(string)
	return subjectID
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
