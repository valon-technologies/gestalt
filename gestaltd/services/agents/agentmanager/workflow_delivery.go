package agentmanager

import (
	"context"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

type inheritedOutputDeliveryKey struct{}

func WithInheritedOutputDelivery(ctx context.Context, delivery *coreworkflow.OutputDelivery) context.Context {
	if delivery == nil {
		return ctx
	}
	return context.WithValue(ctx, inheritedOutputDeliveryKey{}, coreworkflow.CloneOutputDelivery(delivery))
}

func InheritedOutputDeliveryFromContext(ctx context.Context) *coreworkflow.OutputDelivery {
	if ctx == nil {
		return nil
	}
	delivery, _ := ctx.Value(inheritedOutputDeliveryKey{}).(*coreworkflow.OutputDelivery)
	return coreworkflow.CloneOutputDelivery(delivery)
}
