package bootstrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type lazyInvoker struct {
	mu     sync.RWMutex
	target invocation.Invoker
}

func newLazyInvoker() *lazyInvoker {
	return &lazyInvoker{}
}

func (l *lazyInvoker) SetTarget(target invocation.Invoker) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

func (l *lazyInvoker) Invoke(ctx context.Context, p *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("plugin invoker is not available")
	}
	return target.Invoke(ctx, p, providerName, instance, operation, params)
}

func (l *lazyInvoker) InvokeGraphQL(ctx context.Context, p *principal.Principal, providerName, instance string, request invocation.GraphQLRequest) (*core.OperationResult, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("plugin invoker is not available")
	}
	graphQLInvoker, ok := target.(invocation.GraphQLInvoker)
	if !ok {
		return nil, fmt.Errorf("plugin invoker is not available")
	}
	return graphQLInvoker.InvokeGraphQL(ctx, p, providerName, instance, request)
}
