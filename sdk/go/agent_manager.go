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
