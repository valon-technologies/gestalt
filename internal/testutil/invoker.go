package testutil

import (
	"context"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
)

type StubInvoker struct {
	InvokeFn func(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error)

	Invoked   bool
	LastCtx   context.Context
	LastP     *principal.Principal
	Provider  string
	Instance  string
	Operation string
	Params    map[string]any

	Result *core.OperationResult
	Err    error
}

func (s *StubInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	s.Invoked = true
	s.LastCtx = ctx
	s.LastP = p
	s.Provider = providerName
	s.Instance = instance
	s.Operation = operation
	s.Params = params

	if s.InvokeFn != nil {
		return s.InvokeFn(ctx, p, providerName, instance, operation, params)
	}
	if s.Err != nil {
		return nil, s.Err
	}
	return s.Result, nil
}
