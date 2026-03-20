package invocation

import (
	"context"

	"github.com/google/uuid"
)

type InvocationMeta struct {
	RequestID string
	Depth     int
	CallChain []string // "provider/operation" entries
}

type invocationMetaKey struct{}

func MetaFromContext(ctx context.Context) *InvocationMeta {
	meta, _ := ctx.Value(invocationMetaKey{}).(*InvocationMeta)
	return meta
}

func ContextWithMeta(ctx context.Context, meta *InvocationMeta) context.Context {
	return context.WithValue(ctx, invocationMetaKey{}, meta)
}

func ensureMeta(ctx context.Context) (context.Context, *InvocationMeta) {
	meta := MetaFromContext(ctx)
	if meta != nil {
		return ctx, meta
	}
	meta = &InvocationMeta{RequestID: uuid.NewString()}
	return ContextWithMeta(ctx, meta), meta
}
