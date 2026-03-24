package egress

import "context"

// PolicyInput is the normalized request context that policy engines can inspect
// regardless of whether the call came from a provider invocation or a proxy.
type PolicyInput struct {
	Subject Subject
	Target  Target
	Headers map[string]string
}

// PolicyEnforcer decides whether a normalized outbound request is allowed.
type PolicyEnforcer interface {
	Evaluate(ctx context.Context, input PolicyInput) error
}

// EvaluatePolicy is nil-safe so callers can add policy later without branching.
func EvaluatePolicy(ctx context.Context, enforcer PolicyEnforcer, input PolicyInput) error {
	if enforcer == nil {
		return nil
	}
	return enforcer.Evaluate(ctx, input)
}
