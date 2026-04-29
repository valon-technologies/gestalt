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

func (l *lazyAgentManager) Available() bool {
	target, err := l.current()
	if err != nil {
		return false
	}
	return target.Available()
}

func (l *lazyAgentManager) ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	target, err := l.current()
	if err != nil {
		return coreagent.Tool{}, err
	}
	return target.ResolveTool(ctx, p, ref)
}

func (l *lazyAgentManager) ResolveTools(ctx context.Context, p *principal.Principal, req coreagent.ResolveToolsRequest) ([]coreagent.Tool, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ResolveTools(ctx, p, req)
}

func (l *lazyAgentManager) SearchTools(ctx context.Context, p *principal.Principal, req coreagent.SearchToolsRequest) (*coreagent.SearchToolsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.SearchTools(ctx, p, req)
}

func (l *lazyAgentManager) CreateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateSession(ctx, p, req)
}

func (l *lazyAgentManager) GetSession(ctx context.Context, p *principal.Principal, sessionID string) (*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetSession(ctx, p, sessionID)
}

func (l *lazyAgentManager) ListSessions(ctx context.Context, p *principal.Principal, providerName string) ([]*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListSessions(ctx, p, providerName)
}

func (l *lazyAgentManager) UpdateSession(ctx context.Context, p *principal.Principal, req coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.UpdateSession(ctx, p, req)
}

func (l *lazyAgentManager) CreateTurn(ctx context.Context, p *principal.Principal, req coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateTurn(ctx, p, req)
}

func (l *lazyAgentManager) GetTurn(ctx context.Context, p *principal.Principal, turnID string) (*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetTurn(ctx, p, turnID)
}

func (l *lazyAgentManager) ListTurns(ctx context.Context, p *principal.Principal, sessionID string) ([]*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListTurns(ctx, p, sessionID)
}

func (l *lazyAgentManager) CancelTurn(ctx context.Context, p *principal.Principal, turnID, reason string) (*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CancelTurn(ctx, p, turnID, reason)
}

func (l *lazyAgentManager) ListTurnEvents(ctx context.Context, p *principal.Principal, turnID string, afterSeq int64, limit int) ([]*coreagent.TurnEvent, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListTurnEvents(ctx, p, turnID, afterSeq, limit)
}

func (l *lazyAgentManager) ListInteractions(ctx context.Context, p *principal.Principal, turnID string) ([]*coreagent.Interaction, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListInteractions(ctx, p, turnID)
}

func (l *lazyAgentManager) ResolveInteraction(ctx context.Context, p *principal.Principal, turnID, interactionID string, resolution map[string]any) (*coreagent.Interaction, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ResolveInteraction(ctx, p, turnID, interactionID, resolution)
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
