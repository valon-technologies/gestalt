package agentmanager

import (
	"context"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

type inheritedOutputDeliveryKey struct{}

func WithInheritedOutputDelivery(ctx context.Context, delivery *coreworkflow.StepDelivery) context.Context {
	if delivery == nil {
		return ctx
	}
	return context.WithValue(ctx, inheritedOutputDeliveryKey{}, coreworkflow.CloneStepDelivery(delivery))
}

func InheritedOutputDeliveryFromContext(ctx context.Context) *coreworkflow.StepDelivery {
	if ctx == nil {
		return nil
	}
	delivery, _ := ctx.Value(inheritedOutputDeliveryKey{}).(*coreworkflow.StepDelivery)
	return coreworkflow.CloneStepDelivery(delivery)
}
