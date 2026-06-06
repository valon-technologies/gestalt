package gestalt

import (
	"context"
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type agent struct {
	client  proto.AgentProviderClient
	context *proto.RequestContext
}

// Agent is the fakeable contract for managing agent sessions, turns, events, and interactions.
type Agent interface {
	Close() error
	CreateSession(context.Context, AgentCreateSession) (*AgentSession, error)
	GetSession(context.Context, AgentGetSession) (*AgentSession, error)
	ListSessions(context.Context, AgentListSessions) (*ListAgentSessionsResponse, error)
	UpdateSession(context.Context, AgentUpdateSession) (*AgentSession, error)
	CreateTurn(context.Context, AgentCreateTurn) (*AgentTurn, error)
	GetTurn(context.Context, AgentGetTurn) (*AgentTurn, error)
	ListTurns(context.Context, AgentListTurns) (*ListAgentTurnsResponse, error)
	CancelTurn(context.Context, AgentCancelTurn) (*AgentTurn, error)
	ListTurnEvents(context.Context, AgentListTurnEvents) (*ListAgentTurnEventsResponse, error)
	ListInteractions(context.Context, AgentListInteractions) (*ListAgentInteractionsResponse, error)
	ResolveInteraction(context.Context, AgentResolveInteraction) (*AgentInteraction, error)
}

var sharedAgentTransport sharedManagerTransport[proto.AgentProviderClient]

// NewAgent returns an agent capability for the current provider request.
func NewAgent(req Request) (Agent, error) {
	reqCtx, err := requestContextForRequest(req)
	if err != nil {
		return nil, err
	}
	return newAgent(reqCtx)
}

// NewAgentFromRequest returns an agent capability for the current provider request.
func NewAgentFromRequest(req Request) (Agent, error) {
	return NewAgent(req)
}

func newAgent(reqCtx *proto.RequestContext) (Agent, error) {
	target, token, err := hostServiceTarget("agent")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "agent", target, token, &sharedAgentTransport, proto.NewAgentProviderClient)
	if err != nil {
		return nil, err
	}

	return &agent{
		client:  client,
		context: cloneRequestContext(reqCtx),
	}, nil
}

// AgentFromContext returns an Agent using the current provider request context.
func AgentFromContext(ctx context.Context) (Agent, error) {
	return newAgent(requestContextFromContext(ctx))
}

// Close is a no-op because this capability uses shared transport.
func (c *agent) Close() error {
	return nil
}

func (c *agent) attachAuth(ctx context.Context, req protoMessage) error {
	reqCtx, err := requestContextForAgentCall(ctx, c.context)
	if err != nil {
		return err
	}
	attachHostServiceAuth(req, reqCtx)
	return attachWorkflowContextField(ctx, req)
}

// CreateSession creates an agent session.
func (c *agent) CreateSession(ctx context.Context, input AgentCreateSession) (*AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req, err := newAgentCreateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.CreateSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp), nil
}

// GetSession fetches one agent session.
func (c *agent) GetSession(ctx context.Context, input AgentGetSession) (*AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentGetSessionRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp), nil
}

// ListSessions lists agent sessions visible to the current request context.
func (c *agent) ListSessions(ctx context.Context, input AgentListSessions) (*ListAgentSessionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentListSessionsRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.ListSessions(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentSessionsResponseFromProto(resp), nil
}

// UpdateSession updates mutable fields on an agent session.
func (c *agent) UpdateSession(ctx context.Context, input AgentUpdateSession) (*AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req, err := newAgentUpdateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.UpdateSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp), nil
}

// CreateTurn creates an agent turn.
func (c *agent) CreateTurn(ctx context.Context, input AgentCreateTurn) (*AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req, err := newAgentCreateTurnRequest(input)
	if err != nil {
		return nil, err
	}
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.CreateTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp), nil
}

// GetTurn fetches one agent turn.
func (c *agent) GetTurn(ctx context.Context, input AgentGetTurn) (*AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentGetTurnRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.GetTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp), nil
}

// ListTurns lists turns for an agent session.
func (c *agent) ListTurns(ctx context.Context, input AgentListTurns) (*ListAgentTurnsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentListTurnsRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.ListTurns(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentTurnsResponseFromProto(resp), nil
}

// CancelTurn cancels an in-progress agent turn.
func (c *agent) CancelTurn(ctx context.Context, input AgentCancelTurn) (*AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentCancelTurnRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.CancelTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp), nil
}

// ListTurnEvents lists events emitted for an agent turn.
func (c *agent) ListTurnEvents(ctx context.Context, input AgentListTurnEvents) (*ListAgentTurnEventsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentListTurnEventsRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.ListTurnEvents(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentTurnEventsResponseFromProto(resp), nil
}

// ListInteractions lists pending or completed agent interactions.
func (c *agent) ListInteractions(ctx context.Context, input AgentListInteractions) (*ListAgentInteractionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req := newAgentListInteractionsRequest(input)
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.ListInteractions(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentInteractionsResponseFromProto(resp), nil
}

// ResolveInteraction resolves an agent interaction with a host response.
func (c *agent) ResolveInteraction(ctx context.Context, input AgentResolveInteraction) (*AgentInteraction, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent: client is not initialized")
	}
	req, err := newAgentResolveInteractionRequest(input)
	if err != nil {
		return nil, err
	}
	if err := c.attachAuth(ctx, req); err != nil {
		return nil, err
	}
	resp, err := c.client.ResolveInteraction(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp), nil
}

func requestContextForAgentCall(ctx context.Context, base *proto.RequestContext) (*proto.RequestContext, error) {
	out := cloneRequestContext(base)
	workflow := WorkflowContextFromContext(ctx)
	if workflow == nil {
		return out, nil
	}
	msg, err := structFromAny(workflow)
	if err != nil {
		return nil, fmt.Errorf("agent: encode workflow context: %w", err)
	}
	if out == nil {
		out = &proto.RequestContext{}
	}
	out.Workflow = msg
	return out, nil
}
