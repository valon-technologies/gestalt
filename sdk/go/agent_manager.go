package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
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
func (c *AgentManagerClient) CreateSession(ctx context.Context, input AgentManagerCreateSessionInput) (*proto.AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := NewAgentManagerCreateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.CreateSession(ctx, req)
}

// GetSession fetches one agent session.
func (c *AgentManagerClient) GetSession(ctx context.Context, input AgentManagerGetSessionInput) (*proto.AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerGetSessionRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.GetSession(ctx, req)
}

// ListSessions lists agent sessions visible to the invocation token.
func (c *AgentManagerClient) ListSessions(ctx context.Context, input AgentManagerListSessionsInput) (*proto.AgentManagerListSessionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerListSessionsRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.ListSessions(ctx, req)
}

// UpdateSession updates mutable fields on an agent session.
func (c *AgentManagerClient) UpdateSession(ctx context.Context, input AgentManagerUpdateSessionInput) (*proto.AgentSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := NewAgentManagerUpdateSessionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.UpdateSession(ctx, req)
}

// CreateTurn creates an agent turn.
func (c *AgentManagerClient) CreateTurn(ctx context.Context, input AgentManagerCreateTurnInput) (*proto.AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := NewAgentManagerCreateTurnRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.CreateTurn(ctx, req)
}

// GetTurn fetches one agent turn.
func (c *AgentManagerClient) GetTurn(ctx context.Context, input AgentManagerGetTurnInput) (*proto.AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerGetTurnRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.GetTurn(ctx, req)
}

// ListTurns lists turns for an agent session.
func (c *AgentManagerClient) ListTurns(ctx context.Context, input AgentManagerListTurnsInput) (*proto.AgentManagerListTurnsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerListTurnsRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.ListTurns(ctx, req)
}

// CancelTurn cancels an in-progress agent turn.
func (c *AgentManagerClient) CancelTurn(ctx context.Context, input AgentManagerCancelTurnInput) (*proto.AgentTurn, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerCancelTurnRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.CancelTurn(ctx, req)
}

// ListTurnEvents lists events emitted for an agent turn.
func (c *AgentManagerClient) ListTurnEvents(ctx context.Context, input AgentManagerListTurnEventsInput) (*proto.AgentManagerListTurnEventsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerListTurnEventsRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.ListTurnEvents(ctx, req)
}

// ListInteractions lists pending or completed agent interactions.
func (c *AgentManagerClient) ListInteractions(ctx context.Context, input AgentManagerListInteractionsInput) (*proto.AgentManagerListInteractionsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req := NewAgentManagerListInteractionsRequest(input)
	req.InvocationToken = c.invocationToken
	return c.client.ListInteractions(ctx, req)
}

// ResolveInteraction resolves an agent interaction with a host response.
func (c *AgentManagerClient) ResolveInteraction(ctx context.Context, input AgentManagerResolveInteractionInput) (*proto.AgentInteraction, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	req, err := NewAgentManagerResolveInteractionRequest(input)
	if err != nil {
		return nil, err
	}
	req.InvocationToken = c.invocationToken
	return c.client.ResolveInteraction(ctx, req)
}
