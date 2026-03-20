package broker

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
	m, _ := ctx.Value(invocationMetaKey{}).(*InvocationMeta)
	return m
}

func ContextWithMeta(ctx context.Context, meta *InvocationMeta) context.Context {
	return context.WithValue(ctx, invocationMetaKey{}, meta)
}

func ensureMeta(ctx context.Context) (context.Context, *InvocationMeta) {
	m := MetaFromContext(ctx)
	if m != nil {
		return ctx, m
	}
	m = &InvocationMeta{RequestID: uuid.NewString()}
	return ContextWithMeta(ctx, m), m
}
