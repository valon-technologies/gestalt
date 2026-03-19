package broker

import (
	"context"
	"fmt"

	"github.com/valon-technologies/toolshed/core"
)

var _ core.Broker = (*ScopedBroker)(nil)

type ScopedBroker struct {
	inner   *Broker
	allowed map[string]struct{}
}

func NewScoped(inner *Broker, providers []string) *ScopedBroker {
	allowed := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		allowed[p] = struct{}{}
	}
	return &ScopedBroker{inner: inner, allowed: allowed}
}

func (s *ScopedBroker) Invoke(ctx context.Context, req core.InvocationRequest) (*core.OperationResult, error) {
	if _, ok := s.allowed[req.Provider]; !ok {
		return nil, fmt.Errorf("provider %q is not available in this scope", req.Provider)
	}
	return s.inner.Invoke(ctx, req)
}

func (s *ScopedBroker) ListCapabilities() []core.Capability {
	all := s.inner.ListCapabilities()
	var filtered []core.Capability
	for _, cap := range all {
		if _, ok := s.allowed[cap.Provider]; ok {
			filtered = append(filtered, cap)
		}
	}
	return filtered
}
