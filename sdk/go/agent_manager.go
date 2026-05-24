package gestalt

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// AgentManagerClient manages agent sessions, turns, events, and interactions.
type AgentManagerClient struct {
	client          proto.AgentProviderClient
	invocationToken string
}

// AgentManagerClientContract is the fakeable client contract for managing
// agent sessions, turns, events, and interactions.
type AgentManagerClientContract interface {
	Close() error
	CreateSession(context.Context, AgentManagerCreateSession) (*AgentSession, error)
	GetSession(context.Context, AgentManagerGetSession) (*AgentSession, error)
	ListSessions(context.Context, AgentManagerListSessions) (*ListAgentManagerSessionsResponse, error)
	UpdateSession(context.Context, AgentManagerUpdateSession) (*AgentSession, error)
	CreateTurn(context.Context, AgentManagerCreateTurn) (*AgentTurn, error)
	GetTurn(context.Context, AgentManagerGetTurn) (*AgentTurn, error)
	ListTurns(context.Context, AgentManagerListTurns) (*ListAgentManagerTurnsResponse, error)
	CancelTurn(context.Context, AgentManagerCancelTurn) (*AgentTurn, error)
	ListTurnEvents(context.Context, AgentManagerListTurnEvents) (*ListAgentManagerTurnEventsResponse, error)
	ListInteractions(context.Context, AgentManagerListInteractions) (*ListAgentManagerInteractionsResponse, error)
	ResolveInteraction(context.Context, AgentManagerResolveInteraction) (*AgentInteraction, error)
}

var sharedAgentManagerTransport sharedManagerTransport[proto.AgentProviderClient]

// AgentManager returns a client that attaches invocationToken to every request.
func AgentManager(invocationToken string) (*AgentManagerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("agent manager: invocation token is not available")
	}
	target, token, err := hostServiceTarget("agent manager")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "agent manager", target, token, &sharedAgentManagerTransport, proto.NewAgentProviderClient)
	if err != nil {
		return nil, err
	}

	return &AgentManagerClient{client: client, invocationToken: strings.TrimSpace(invocationToken)}, nil
}

// AgentManagerFromContext returns an AgentManager using the context invocation token.
func AgentManagerFromContext(ctx context.Context) (*AgentManagerClient, error) {
	return AgentManager(InvocationTokenFromContext(ctx))
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *AgentManagerClient) Close() error {
	return nil
}

// CreateSession creates an agent session.
func (c *AgentManagerClient) CreateSession(ctx context.Context, input AgentManagerCreateSession) (*AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := newAgentManagerCreateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.CreateSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp), nil
}

// GetSession fetches one agent session.
func (c *AgentManagerClient) GetSession(ctx context.Context, input AgentManagerGetSession) (*AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerGetSessionRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp), nil
}

// ListSessions lists agent sessions visible to the invocation token.
func (c *AgentManagerClient) ListSessions(ctx context.Context, input AgentManagerListSessions) (*ListAgentManagerSessionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerListSessionsRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ListSessions(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentManagerSessionsResponseFromProto(resp), nil
}

// UpdateSession updates mutable fields on an agent session.
func (c *AgentManagerClient) UpdateSession(ctx context.Context, input AgentManagerUpdateSession) (*AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := newAgentManagerUpdateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.UpdateSession(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentSessionFromProto(resp), nil
}

// CreateTurn creates an agent turn.
func (c *AgentManagerClient) CreateTurn(ctx context.Context, input AgentManagerCreateTurn) (*AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := newAgentManagerCreateTurnRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.CreateTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp), nil
}

// GetTurn fetches one agent turn.
func (c *AgentManagerClient) GetTurn(ctx context.Context, input AgentManagerGetTurn) (*AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerGetTurnRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.GetTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp), nil
}

// ListTurns lists turns for an agent session.
func (c *AgentManagerClient) ListTurns(ctx context.Context, input AgentManagerListTurns) (*ListAgentManagerTurnsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerListTurnsRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ListTurns(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentManagerTurnsResponseFromProto(resp), nil
}

// CancelTurn cancels an in-progress agent turn.
func (c *AgentManagerClient) CancelTurn(ctx context.Context, input AgentManagerCancelTurn) (*AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerCancelTurnRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.CancelTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentTurnFromProto(resp), nil
}

// ListTurnEvents lists events emitted for an agent turn.
func (c *AgentManagerClient) ListTurnEvents(ctx context.Context, input AgentManagerListTurnEvents) (*ListAgentManagerTurnEventsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerListTurnEventsRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ListTurnEvents(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentManagerTurnEventsResponseFromProto(resp), nil
}

// ListInteractions lists pending or completed agent interactions.
func (c *AgentManagerClient) ListInteractions(ctx context.Context, input AgentManagerListInteractions) (*ListAgentManagerInteractionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := newAgentManagerListInteractionsRequest(input)
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ListInteractions(ctx, req)
	if err != nil {
		return nil, err
	}
	return listAgentManagerInteractionsResponseFromProto(resp), nil
}

// ResolveInteraction resolves an agent interaction with a host response.
func (c *AgentManagerClient) ResolveInteraction(ctx context.Context, input AgentManagerResolveInteraction) (*AgentInteraction, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := newAgentManagerResolveInteractionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	resp, err := c.client.ResolveInteraction(ctx, req)
	if err != nil {
		return nil, err
	}
	return agentInteractionFromProto(resp), nil
}
