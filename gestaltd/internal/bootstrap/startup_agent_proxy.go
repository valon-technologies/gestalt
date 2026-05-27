package bootstrap

import (
	"context"
	"fmt"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type startupAgentProviderProxy struct {
	providerName string
	tracker      *startupWaitTracker
	gate         startupGate[coreagent.Provider]
}

func newStartupAgentProviderProxy(providerName string, tracker *startupWaitTracker) *startupAgentProviderProxy {
	return &startupAgentProviderProxy{
		providerName: providerName,
		tracker:      tracker,
		gate:         newStartupGate[coreagent.Provider](),
	}
}

func (p *startupAgentProviderProxy) publish(provider coreagent.Provider) {
	p.finish(provider, nil)
}

func (p *startupAgentProviderProxy) fail(err error) {
	p.finish(nil, err)
}

func (p *startupAgentProviderProxy) finish(provider coreagent.Provider, err error) {
	if err == nil && provider == nil {
		err = fmt.Errorf("agent provider %q is not available", p.providerName)
	}
	p.gate.finish(provider, err)
}

func (p *startupAgentProviderProxy) await(ctx context.Context) (coreagent.Provider, error) {
	provider, err := p.gate.await(ctx)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("agent provider %q is not available", p.providerName)
	}
	return provider, nil
}

func (p *startupAgentProviderProxy) awaitForCaller(ctx context.Context) (coreagent.Provider, error) {
	done, err := p.beginCallerWait(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return p.await(ctx)
}

func (p *startupAgentProviderProxy) beginCallerWait(ctx context.Context) (func(), error) {
	if p == nil || p.tracker == nil {
		return func() {}, nil
	}
	done, _, err := p.tracker.beginCallerProviderWait(ctx, newStartupProviderNode(invocation.ProviderKindAgent, p.providerName))
	return done, err
}

func (p *startupAgentProviderProxy) SupportsWorkspaceRequests() bool {
	provider, ready, _ := p.gate.resolved()
	if !ready {
		return true
	}
	workspaceProvider, ok := provider.(coreagent.WorkspaceProvider)
	return ok && workspaceProvider.SupportsWorkspaceRequests()
}

func (p *startupAgentProviderProxy) CreateSession(ctx context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	if req.Workspace != nil {
		workspaceProvider, ok := provider.(coreagent.WorkspaceProvider)
		if !ok || !workspaceProvider.SupportsWorkspaceRequests() {
			return nil, fmt.Errorf("%w: provider %q", agentmanager.ErrAgentWorkspaceUnsupported, p.providerName)
		}
	}
	return provider.CreateSession(ctx, req)
}

func (p *startupAgentProviderProxy) GetSession(ctx context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetSession(ctx, req)
}

func (p *startupAgentProviderProxy) ListSessions(ctx context.Context, req coreagent.ListSessionsRequest) ([]*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListSessions(ctx, req)
}

func (p *startupAgentProviderProxy) UpdateSession(ctx context.Context, req coreagent.UpdateSessionRequest) (*coreagent.Session, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.UpdateSession(ctx, req)
}

func (p *startupAgentProviderProxy) CreateTurn(ctx context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CreateTurn(ctx, req)
}

func (p *startupAgentProviderProxy) GetTurn(ctx context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetTurn(ctx, req)
}

func (p *startupAgentProviderProxy) ListTurns(ctx context.Context, req coreagent.ListTurnsRequest) ([]*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListTurns(ctx, req)
}

func (p *startupAgentProviderProxy) CancelTurn(ctx context.Context, req coreagent.CancelTurnRequest) (*coreagent.Turn, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.CancelTurn(ctx, req)
}

func (p *startupAgentProviderProxy) ListTurnEvents(ctx context.Context, req coreagent.ListTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListTurnEvents(ctx, req)
}

func (p *startupAgentProviderProxy) GetInteraction(ctx context.Context, req coreagent.GetInteractionRequest) (*coreagent.Interaction, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetInteraction(ctx, req)
}

func (p *startupAgentProviderProxy) ListInteractions(ctx context.Context, req coreagent.ListInteractionsRequest) ([]*coreagent.Interaction, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ListInteractions(ctx, req)
}

func (p *startupAgentProviderProxy) ResolveInteraction(ctx context.Context, req coreagent.ResolveInteractionRequest) (*coreagent.Interaction, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.ResolveInteraction(ctx, req)
}

func (p *startupAgentProviderProxy) GetCapabilities(ctx context.Context, req coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	provider, err := p.awaitForCaller(ctx)
	if err != nil {
		return nil, err
	}
	return provider.GetCapabilities(ctx, req)
}

func (p *startupAgentProviderProxy) Ping(ctx context.Context) error {
	provider, ready, err := p.gate.resolved()
	if !ready {
		return agentmanager.NewAgentProviderNotAvailableError(p.providerName)
	}
	if err != nil {
		return err
	}
	if provider == nil {
		return agentmanager.NewAgentProviderNotAvailableError(p.providerName)
	}
	return provider.Ping(ctx)
}

func (p *startupAgentProviderProxy) Close() error {
	provider, ready, _ := p.gate.resolved()
	if !ready || provider == nil {
		return nil
	}
	return provider.Close()
}
