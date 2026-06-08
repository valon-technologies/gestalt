package bootstrap

import (
	"context"
	"fmt"
	"sync"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/agents/agentturnscope"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type lazyAgentManager struct {
	mu     sync.RWMutex
	target *agentmanager.Manager
}

func newLazyAgentManager() *lazyAgentManager {
	return &lazyAgentManager{}
}

func (l *lazyAgentManager) SetTarget(target *agentmanager.Manager) {
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

func (l *lazyAgentManager) ListTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListTools(ctx, p, req)
}

func (l *lazyAgentManager) CreateSession(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateSession(ctx, p, req)
}

func (l *lazyAgentManager) GetSession(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetSession(ctx, p, req)
}

func (l *lazyAgentManager) ListSessions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListSessions(ctx, p, req)
}

func (l *lazyAgentManager) UpdateSession(ctx context.Context, p *principal.Principal, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.UpdateSession(ctx, p, req)
}

func (l *lazyAgentManager) CreateTurn(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateTurn(ctx, p, req)
}

func (l *lazyAgentManager) GetTurn(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetTurn(ctx, p, req)
}

func (l *lazyAgentManager) ListTurns(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListTurns(ctx, p, req)
}

func (l *lazyAgentManager) CancelTurn(ctx context.Context, p *principal.Principal, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CancelTurn(ctx, p, req)
}

func (l *lazyAgentManager) ListTurnEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListTurnEvents(ctx, p, req)
}

func (l *lazyAgentManager) ListInteractions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListInteractions(ctx, p, req)
}

func (l *lazyAgentManager) ResolveInteraction(ctx context.Context, p *principal.Principal, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ResolveInteraction(ctx, p, req)
}

func (l *lazyAgentManager) AuthorizeAgentAppInvocation(ctx context.Context, providerName, turnID string, requestPrincipal *principal.Principal, targetTool coreagent.ToolTarget, reqCtx *proto.RequestContext) (*principal.Principal, agentturnscope.Scope, coreagent.ListedTool, error) {
	target, err := l.current()
	if err != nil {
		return nil, agentturnscope.Scope{}, coreagent.ListedTool{}, err
	}
	return target.AuthorizeAgentAppInvocation(ctx, providerName, turnID, requestPrincipal, targetTool, reqCtx)
}

func (l *lazyAgentManager) current() (*agentmanager.Manager, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("agent manager is not available")
	}
	return target, nil
}

var _ agentmanager.Service = (*lazyAgentManager)(nil)
