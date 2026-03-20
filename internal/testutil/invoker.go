package testutil

import (
	"context"

	"github.com/valon-technologies/toolshed/core"
	"github.com/valon-technologies/toolshed/internal/principal"
)

// StubInvoker is a configurable test double for invocation.Invoker.
// When InvokeFn is set, Invoke delegates to it; otherwise it returns nil, nil.
// All calls record state in the exported fields for assertion.
type StubInvoker struct {
	InvokeFn func(ctx context.Context, p *principal.Principal, providerName, operation string, params map[string]any) (*core.OperationResult, error)

	Invoked   bool
	LastCtx   context.Context
	LastP     *principal.Principal
	Provider  string
	Operation string
	Params    map[string]any

	Result *core.OperationResult
	Err    error
}

func (s *StubInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, operation string, params map[string]any) (*core.OperationResult, error) {
	s.Invoked = true
	s.LastCtx = ctx
	s.LastP = p
	s.Provider = providerName
	s.Operation = operation
	s.Params = params

	if s.InvokeFn != nil {
		return s.InvokeFn(ctx, p, providerName, operation, params)
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Result, nil
}
