package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	gproto "google.golang.org/protobuf/proto"
)

const EnvAgentManagerSocket = proto.EnvAgentManagerSocket
const EnvAgentManagerSocketToken = EnvAgentManagerSocket + "_TOKEN"

type AgentManagerClient struct {
	client          proto.AgentManagerHostClient
	invocationToken string
}

var sharedAgentManagerTransport sharedManagerTransport[proto.AgentManagerHostClient]

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

func AgentManagerFromContext(ctx context.Context) (*AgentManagerClient, error) {
	return AgentManager(InvocationTokenFromContext(ctx))
}

func (c *AgentManagerClient) Close() error {
	return nil
}

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
