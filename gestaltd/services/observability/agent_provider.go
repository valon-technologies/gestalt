package observability

import (
	"context"
	"strings"
	"time"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"go.opentelemetry.io/otel/attribute"
)

type observedAgentProvider struct {
	name     string
	delegate coreagent.Provider
}

func InstrumentAgentProvider(name string, provider coreagent.Provider) coreagent.Provider {
	if provider == nil {
		return nil
	}
	if _, ok := provider.(*observedAgentProvider); ok {
		return provider
	}
	return &observedAgentProvider{
		name:     strings.TrimSpace(name),
		delegate: provider,
	}
}

func (p *observedAgentProvider) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, end := p.start(ctx, "create_session")
	defer func() { end(err) }()
	return p.delegate.CreateSession(ctx, req)
}

func (p *observedAgentProvider) GetSession(ctx context.Context, req *proto.GetAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, end := p.start(ctx, "get_session")
	defer func() { end(err) }()
	return p.delegate.GetSession(ctx, req)
}

func (p *observedAgentProvider) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (sessions []*coreagent.Session, err error) {
	ctx, end := p.start(ctx, "list_sessions")
	defer func() { end(err) }()
	return p.delegate.ListSessions(ctx, req)
}

func (p *observedAgentProvider) UpdateSession(ctx context.Context, req *proto.UpdateAgentProviderSessionRequest) (session *coreagent.Session, err error) {
	ctx, end := p.start(ctx, "update_session")
	defer func() { end(err) }()
	return p.delegate.UpdateSession(ctx, req)
}

func (p *observedAgentProvider) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, end := p.start(ctx, "create_turn")
	defer func() { end(err) }()
	return p.delegate.CreateTurn(ctx, req)
}

func (p *observedAgentProvider) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, end := p.start(ctx, "get_turn")
	defer func() { end(err) }()
	return p.delegate.GetTurn(ctx, req)
}

func (p *observedAgentProvider) ListTurns(ctx context.Context, req *proto.ListAgentProviderTurnsRequest) (turns []*coreagent.Turn, err error) {
	ctx, end := p.start(ctx, "list_turns")
	defer func() { end(err) }()
	return p.delegate.ListTurns(ctx, req)
}

func (p *observedAgentProvider) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (turn *coreagent.Turn, err error) {
	ctx, end := p.start(ctx, "cancel_turn")
	defer func() { end(err) }()
	return p.delegate.CancelTurn(ctx, req)
}

func (p *observedAgentProvider) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest) (events []*coreagent.TurnEvent, err error) {
	ctx, end := p.start(ctx, "list_turn_events")
	defer func() { end(err) }()
	return p.delegate.ListTurnEvents(ctx, req)
}

func (p *observedAgentProvider) GetInteraction(ctx context.Context, req *proto.GetAgentProviderInteractionRequest) (interaction *coreagent.Interaction, err error) {
	ctx, end := p.start(ctx, "get_interaction")
	defer func() { end(err) }()
	return p.delegate.GetInteraction(ctx, req)
}

func (p *observedAgentProvider) ListInteractions(ctx context.Context, req *proto.ListAgentProviderInteractionsRequest) (interactions []*coreagent.Interaction, err error) {
	ctx, end := p.start(ctx, "list_interactions")
	defer func() { end(err) }()
	return p.delegate.ListInteractions(ctx, req)
}

func (p *observedAgentProvider) ResolveInteraction(ctx context.Context, req *proto.ResolveAgentProviderInteractionRequest) (interaction *coreagent.Interaction, err error) {
	ctx, end := p.start(ctx, "resolve_interaction")
	defer func() { end(err) }()
	return p.delegate.ResolveInteraction(ctx, req)
}

func (p *observedAgentProvider) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest) (caps *coreagent.ProviderCapabilities, err error) {
	ctx, end := p.start(ctx, "get_capabilities")
	defer func() { end(err) }()
	return p.delegate.GetCapabilities(ctx, req)
}

func (p *observedAgentProvider) Ping(ctx context.Context) (err error) {
	ctx, end := p.start(ctx, "ping")
	defer func() { end(err) }()
	return p.delegate.Ping(ctx)
}

func (p *observedAgentProvider) Close() error {
	return p.delegate.Close()
}

func (p *observedAgentProvider) Unwrap() coreagent.Provider {
	return p.delegate
}

func (p *observedAgentProvider) SupportsWorkspaceRequests() bool {
	workspaceProvider, ok := p.delegate.(coreagent.WorkspaceProvider)
	return ok && workspaceProvider.SupportsWorkspaceRequests()
}

func (p *observedAgentProvider) start(ctx context.Context, operation string) (context.Context, func(error)) {
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		AttrAgentProvider.String(p.name),
		AttrAgentOperation.String(operation),
	}
	ctx, span := StartSpan(ctx, "agent.provider.operation", attrs...)
	return ctx, func(err error) {
		EndSpan(span, err)
		RecordAgentProviderOperation(ctx, startedAt, err != nil, attrs...)
	}
}
