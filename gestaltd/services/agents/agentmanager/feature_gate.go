package agentmanager

import (
	"context"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/featureflags"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type featureGate struct {
	enabled bool
	next    Service
}

func NewFeatureGate(enabled bool, next Service) Service {
	return &featureGate{enabled: enabled, next: next}
}

func (g *featureGate) disabled() error {
	if g != nil && g.enabled {
		return nil
	}
	return featureflags.NewDisabledError(featureflags.Agent)
}

func (g *featureGate) Available() bool {
	return g != nil && g.enabled && g.next != nil && g.next.Available()
}

func (g *featureGate) ResolveTool(ctx context.Context, p *principal.Principal, ref coreagent.ToolRef) (coreagent.Tool, error) {
	if err := g.disabled(); err != nil {
		return coreagent.Tool{}, err
	}
	return g.next.ResolveTool(ctx, p, ref)
}

func (g *featureGate) ResolveTools(ctx context.Context, p *principal.Principal, req coreagent.ResolveToolsRequest) ([]coreagent.Tool, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ResolveTools(ctx, p, req)
}

func (g *featureGate) ListTools(ctx context.Context, p *principal.Principal, req coreagent.ListToolsRequest) (*coreagent.ListToolsResponse, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListTools(ctx, p, req)
}

func (g *featureGate) CreateSession(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.CreateSession(ctx, p, req)
}

func (g *featureGate) GetSession(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.GetSession(ctx, p, req)
}

func (g *featureGate) ListSessions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListSessions(ctx, p, req)
}

func (g *featureGate) UpdateSession(ctx context.Context, p *principal.Principal, req *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.UpdateSession(ctx, p, req)
}

func (g *featureGate) CreateTurn(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.CreateTurn(ctx, p, req)
}

func (g *featureGate) GetTurn(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.GetTurn(ctx, p, req)
}

func (g *featureGate) ListTurns(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListTurns(ctx, p, req)
}

func (g *featureGate) CancelTurn(ctx context.Context, p *principal.Principal, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.CancelTurn(ctx, p, req)
}

func (g *featureGate) ListTurnEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListTurnEvents(ctx, p, req)
}

func (g *featureGate) ListInteractions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ListInteractions(ctx, p, req)
}

func (g *featureGate) ResolveInteraction(ctx context.Context, p *principal.Principal, req *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	if err := g.disabled(); err != nil {
		return nil, err
	}
	return g.next.ResolveInteraction(ctx, p, req)
}

func (g *featureGate) AuthorizeAppInvocation(ctx context.Context, req invocation.AgentAppAuthorizationRequest) (invocation.AgentAppAuthorization, error) {
	if err := g.disabled(); err != nil {
		return invocation.AgentAppAuthorization{}, err
	}
	return g.next.AuthorizeAppInvocation(ctx, req)
}

func (g *featureGate) AuthorizeWorkflowInvocation(ctx context.Context, req invocation.AgentWorkflowAuthorizationRequest) (invocation.AgentWorkflowAuthorization, error) {
	if err := g.disabled(); err != nil {
		return invocation.AgentWorkflowAuthorization{}, err
	}
	return g.next.AuthorizeWorkflowInvocation(ctx, req)
}

var _ Service = (*featureGate)(nil)
