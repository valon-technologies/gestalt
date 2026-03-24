package egresstest

import (
	"context"

	"github.com/valon-technologies/gestalt/internal/egress"
)

// PolicyFunc adapts a plain function to the egress.PolicyEnforcer interface.
type PolicyFunc func(context.Context, egress.PolicyInput) error

func (f PolicyFunc) Evaluate(ctx context.Context, input egress.PolicyInput) error {
	return f(ctx, input)
}
