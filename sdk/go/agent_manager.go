package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	gproto "google.golang.org/protobuf/proto"
)

// EnvAgentManagerSocket names the environment variable containing the
// agent-manager service target.
const EnvAgentManagerSocket = proto.EnvAgentManagerSocket

// EnvAgentManagerSocketToken names the optional agent-manager relay-token
// variable.
const EnvAgentManagerSocketToken = EnvAgentManagerSocket + "_TOKEN"

// AgentManagerClient manages agent sessions, turns, events, and interactions.
type AgentManagerClient struct {
	client          proto.AgentManagerHostClient
	invocationToken string
}

var sharedAgentManagerTransport sharedManagerTransport[proto.AgentManagerHostClient]

// AgentManager returns a client that attaches invocationToken to every request.
func AgentManager(invocationToken string) (*AgentManagerClient, error) {
	if strings.TrimSpace(invocationToken) == "" {
		return nil, fmt.Errorf("agent manager: invocation token is not available")
	}
	target := os.Getenv(EnvAgentManagerSocket)
	if target == "" {
		return nil, fmt.Errorf("agent manager: %s is not set", EnvAgentManagerSocket)
	}
	token := os.Getenv(EnvAgentManagerSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "agent manager", target, token, &sharedAgentManagerTransport, proto.NewAgentManagerHostClient)
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
func (c *AgentManagerClient) CreateSession(ctx context.Context, req *proto.AgentManagerCreateSessionRequest) (*proto.AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerCreateSessionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerCreateSessionRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CreateSession(ctx, value)
}

// CreateSessionWithInput creates an agent session.
func (c *AgentManagerClient) CreateSessionWithInput(ctx context.Context, input AgentManagerCreateSessionInput) (*proto.AgentSession, error) {
	req, err := NewAgentManagerCreateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	return c.CreateSession(ctx, req)
}

// GetSession fetches one agent session.
func (c *AgentManagerClient) GetSession(ctx context.Context, req *proto.AgentManagerGetSessionRequest) (*proto.AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerGetSessionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerGetSessionRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetSession(ctx, value)
}

// GetSessionWithInput fetches one agent session.
func (c *AgentManagerClient) GetSessionWithInput(ctx context.Context, input AgentManagerGetSessionInput) (*proto.AgentSession, error) {
	return c.GetSession(ctx, NewAgentManagerGetSessionRequest(input))
}

// ListSessions lists agent sessions visible to the invocation token.
func (c *AgentManagerClient) ListSessions(ctx context.Context, req *proto.AgentManagerListSessionsRequest) (*proto.AgentManagerListSessionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerListSessionsRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerListSessionsRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ListSessions(ctx, value)
}

// ListSessionsWithInput lists agent sessions.
func (c *AgentManagerClient) ListSessionsWithInput(ctx context.Context, input AgentManagerListSessionsInput) (*proto.AgentManagerListSessionsResponse, error) {
	return c.ListSessions(ctx, NewAgentManagerListSessionsRequest(input))
}

// UpdateSession updates mutable fields on an agent session.
func (c *AgentManagerClient) UpdateSession(ctx context.Context, req *proto.AgentManagerUpdateSessionRequest) (*proto.AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerUpdateSessionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerUpdateSessionRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.UpdateSession(ctx, value)
}

// UpdateSessionWithInput updates an agent session.
func (c *AgentManagerClient) UpdateSessionWithInput(ctx context.Context, input AgentManagerUpdateSessionInput) (*proto.AgentSession, error) {
	req, err := NewAgentManagerUpdateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	return c.UpdateSession(ctx, req)
}

// CreateTurn creates an agent turn.
func (c *AgentManagerClient) CreateTurn(ctx context.Context, req *proto.AgentManagerCreateTurnRequest) (*proto.AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerCreateTurnRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerCreateTurnRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CreateTurn(ctx, value)
}

// CreateTurnWithInput creates an agent turn.
func (c *AgentManagerClient) CreateTurnWithInput(ctx context.Context, input AgentManagerCreateTurnInput) (*proto.AgentTurn, error) {
	req, err := NewAgentManagerCreateTurnRequest(input)
	if err != nil {
		return nil, err
	}
	return c.CreateTurn(ctx, req)
}

// GetTurn fetches one agent turn.
func (c *AgentManagerClient) GetTurn(ctx context.Context, req *proto.AgentManagerGetTurnRequest) (*proto.AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerGetTurnRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerGetTurnRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetTurn(ctx, value)
}

// GetTurnWithInput fetches one agent turn.
func (c *AgentManagerClient) GetTurnWithInput(ctx context.Context, input AgentManagerGetTurnInput) (*proto.AgentTurn, error) {
	return c.GetTurn(ctx, NewAgentManagerGetTurnRequest(input))
}

// ListTurns lists turns for an agent session.
func (c *AgentManagerClient) ListTurns(ctx context.Context, req *proto.AgentManagerListTurnsRequest) (*proto.AgentManagerListTurnsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerListTurnsRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerListTurnsRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ListTurns(ctx, value)
}

// ListTurnsWithInput lists turns.
func (c *AgentManagerClient) ListTurnsWithInput(ctx context.Context, input AgentManagerListTurnsInput) (*proto.AgentManagerListTurnsResponse, error) {
	return c.ListTurns(ctx, NewAgentManagerListTurnsRequest(input))
}

// CancelTurn cancels an in-progress agent turn.
func (c *AgentManagerClient) CancelTurn(ctx context.Context, req *proto.AgentManagerCancelTurnRequest) (*proto.AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerCancelTurnRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerCancelTurnRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CancelTurn(ctx, value)
}

// CancelTurnWithInput cancels an agent turn.
func (c *AgentManagerClient) CancelTurnWithInput(ctx context.Context, input AgentManagerCancelTurnInput) (*proto.AgentTurn, error) {
	return c.CancelTurn(ctx, NewAgentManagerCancelTurnRequest(input))
}

// ListTurnEvents lists events emitted for an agent turn.
func (c *AgentManagerClient) ListTurnEvents(ctx context.Context, req *proto.AgentManagerListTurnEventsRequest) (*proto.AgentManagerListTurnEventsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerListTurnEventsRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerListTurnEventsRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ListTurnEvents(ctx, value)
}

// ListTurnEventsWithInput lists turn events.
func (c *AgentManagerClient) ListTurnEventsWithInput(ctx context.Context, input AgentManagerListTurnEventsInput) (*proto.AgentManagerListTurnEventsResponse, error) {
	return c.ListTurnEvents(ctx, NewAgentManagerListTurnEventsRequest(input))
}

// ListInteractions lists pending or completed agent interactions.
func (c *AgentManagerClient) ListInteractions(ctx context.Context, req *proto.AgentManagerListInteractionsRequest) (*proto.AgentManagerListInteractionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerListInteractionsRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerListInteractionsRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ListInteractions(ctx, value)
}

// ListInteractionsWithInput lists interactions.
func (c *AgentManagerClient) ListInteractionsWithInput(ctx context.Context, input AgentManagerListInteractionsInput) (*proto.AgentManagerListInteractionsResponse, error) {
	return c.ListInteractions(ctx, NewAgentManagerListInteractionsRequest(input))
}

// ResolveInteraction resolves an agent interaction with a host response.
func (c *AgentManagerClient) ResolveInteraction(ctx context.Context, req *proto.AgentManagerResolveInteractionRequest) (*proto.AgentInteraction, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerResolveInteractionRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerResolveInteractionRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ResolveInteraction(ctx, value)
}

// ResolveInteractionWithInput resolves an interaction.
func (c *AgentManagerClient) ResolveInteractionWithInput(ctx context.Context, input AgentManagerResolveInteractionInput) (*proto.AgentInteraction, error) {
	req, err := NewAgentManagerResolveInteractionRequest(input)
	if err != nil {
		return nil, err
	}
	return c.ResolveInteraction(ctx, req)
}
