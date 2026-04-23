package bootstrap

import (
	"context"
	"fmt"
	"sync"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type lazyAgentManager struct {
	mu     sync.RWMutex
	target agentmanager.Service
}

func newLazyAgentManager() *lazyAgentManager {
	return &lazyAgentManager{}
}

func (l *lazyAgentManager) SetTarget(target agentmanager.Service) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

func (l *lazyAgentManager) Run(ctx context.Context, p *principal.Principal, req coreagent.ManagerRunRequest) (*coreagent.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.Run(ctx, p, req)
}

func (l *lazyAgentManager) GetRun(ctx context.Context, p *principal.Principal, runID string) (*coreagent.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRun(ctx, p, runID)
}

func (l *lazyAgentManager) ListRuns(ctx context.Context, p *principal.Principal) ([]*coreagent.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListRuns(ctx, p)
}

func (l *lazyAgentManager) CancelRun(ctx context.Context, p *principal.Principal, runID, reason string) (*coreagent.ManagedRun, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CancelRun(ctx, p, runID, reason)
}

func (l *lazyAgentManager) current() (agentmanager.Service, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("agent manager is not available")
	}
	return target, nil
}

var _ agentmanager.Service = (*lazyAgentManager)(nil)
