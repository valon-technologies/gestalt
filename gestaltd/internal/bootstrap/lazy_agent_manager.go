package bootstrap

import (
	"context"
	"fmt"
	"sync"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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

func (l *lazyAgentManager) CreateAgent(ctx context.Context, p *principal.Principal, req *proto.CreateAgentRequest) (*proto.AgentResource, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateAgent(ctx, p, req)
}

func (l *lazyAgentManager) GetAgent(ctx context.Context, p *principal.Principal, req *proto.GetAgentRequest) (*proto.AgentResource, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetAgent(ctx, p, req)
}

func (l *lazyAgentManager) ListAgents(ctx context.Context, p *principal.Principal, req *proto.ListAgentsRequest) (*proto.ListAgentsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListAgents(ctx, p, req)
}

func (l *lazyAgentManager) ArchiveAgent(ctx context.Context, p *principal.Principal, req *proto.ArchiveAgentRequest) (*proto.AgentResource, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ArchiveAgent(ctx, p, req)
}

func (l *lazyAgentManager) CreateConfigRevision(ctx context.Context, p *principal.Principal, req *proto.CreateAgentConfigRevisionRequest) (*proto.AgentConfigRevision, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateConfigRevision(ctx, p, req)
}

func (l *lazyAgentManager) CreateRun(ctx context.Context, p *principal.Principal, req *proto.CreateAgentRunRequest) (*proto.AgentRunResource, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CreateRun(ctx, p, req)
}

func (l *lazyAgentManager) GetRun(ctx context.Context, p *principal.Principal, req *proto.GetAgentRunRequest) (*proto.AgentRunResource, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRun(ctx, p, req)
}

func (l *lazyAgentManager) ListRuns(ctx context.Context, p *principal.Principal, req *proto.ListAgentRunsRequest) (*proto.ListAgentRunsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListRuns(ctx, p, req)
}

func (l *lazyAgentManager) CancelRun(ctx context.Context, p *principal.Principal, req *proto.CancelAgentRunRequest) (*proto.AgentRunResource, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.CancelRun(ctx, p, req)
}

func (l *lazyAgentManager) ListRunEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentRunEventsRequest) (*proto.ListAgentRunEventsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListRunEvents(ctx, p, req)
}

func (l *lazyAgentManager) GetRunInteraction(ctx context.Context, p *principal.Principal, req *proto.GetAgentRunInteractionRequest) (*proto.AgentRunInteraction, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.GetRunInteraction(ctx, p, req)
}

func (l *lazyAgentManager) ListRunInteractions(ctx context.Context, p *principal.Principal, req *proto.ListAgentRunInteractionsRequest) (*proto.ListAgentRunInteractionsResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ListRunInteractions(ctx, p, req)
}

func (l *lazyAgentManager) ResolveRunInteraction(ctx context.Context, p *principal.Principal, req *proto.ResolveAgentRunInteractionRequest) (*proto.AgentRunInteraction, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.ResolveRunInteraction(ctx, p, req)
}

func (l *lazyAgentManager) AuthorizeAppInvocation(ctx context.Context, req invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error) {
	target, err := l.current()
	if err != nil {
		return invocation.AgentAppAuthorization{}, err
	}
	return target.AuthorizeAppInvocation(ctx, req)
}

func (l *lazyAgentManager) AuthorizeWorkflowInvocation(ctx context.Context, req invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error) {
	target, err := l.current()
	if err != nil {
		return invocation.AgentWorkflowAuthorization{}, err
	}
	return target.AuthorizeWorkflowInvocation(ctx, req)
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
var _ agentmanager.ContractService = (*lazyAgentManager)(nil)
