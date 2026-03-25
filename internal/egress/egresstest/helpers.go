package egresstest

import (
	"context"

	"github.com/valon-technologies/gestalt/internal/egress"
)

type PolicyFunc func(context.Context, egress.PolicyInput) error

func (f PolicyFunc) Evaluate(ctx context.Context, input egress.PolicyInput) error {
	return f(ctx, input)
}
