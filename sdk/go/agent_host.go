package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// EnvAgentHostSocket names the environment variable containing the agent-host
// service target.
const EnvAgentHostSocket = "GESTALT_AGENT_HOST_SOCKET"

// EnvAgentHostSocketToken names the optional agent-host relay-token variable.
const EnvAgentHostSocketToken = EnvAgentHostSocket + "_TOKEN"

// AgentHostClient calls host tool APIs from an agent provider.
type AgentHostClient struct {
	client proto.AgentHostClient
}

// AgentHostListToolsInput contains plain fields for listing tools available to
// one agent turn.
type AgentHostListToolsInput struct {
	SessionID string
	TurnID    string
	RunGrant  string
	PageSize  int32
	PageToken string
	Query     string
}

// AgentHostExecuteToolInput contains plain fields for executing a host tool
// during one agent turn.
type AgentHostExecuteToolInput struct {
	SessionID      string
	TurnID         string
	ToolCallID     string
	ToolID         string
	Arguments      any
	RunGrant       string
	IdempotencyKey string
}

// AgentHostResolveConnectionInput contains plain fields for resolving a
// configured connection during one agent turn.
type AgentHostResolveConnectionInput struct {
	SessionID  string
	TurnID     string
	Connection string
	Instance   string
	RunGrant   string
}

var sharedAgentHostTransport sharedManagerTransport[proto.AgentHostClient]

// AgentHost returns a shared client for the host agent service.
func AgentHost() (*AgentHostClient, error) {
	target := os.Getenv(EnvAgentHostSocket)
	if target == "" {
		return nil, fmt.Errorf("agent host: %s is not set", EnvAgentHostSocket)
	}
	token := os.Getenv(EnvAgentHostSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "agent host", target, token, &sharedAgentHostTransport, proto.NewAgentHostClient)
	if err != nil {
		return nil, err
	}
	return &AgentHostClient{
		client: client,
	}, nil
}

// Close is a no-op compatibility method because this client uses shared transport.
func (c *AgentHostClient) Close() error {
	return nil
}

// ExecuteTool executes a host tool using an agent protocol request.
func (c *AgentHostClient) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	return c.client.ExecuteTool(ctx, req)
}

// ExecuteToolForTurn executes a host tool using plain Go request fields.
func (c *AgentHostClient) ExecuteToolForTurn(ctx context.Context, input AgentHostExecuteToolInput) (*proto.ExecuteAgentToolResponse, error) {
	var arguments *structpb.Struct
	if input.Arguments != nil {
		var err error
		arguments, err = StructFromAny(input.Arguments)
		if err != nil {
			return nil, err
		}
	}
	return c.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
		SessionId:      input.SessionID,
		TurnId:         input.TurnID,
		ToolCallId:     input.ToolCallID,
		ToolId:         input.ToolID,
		Arguments:      arguments,
		RunGrant:       input.RunGrant,
		IdempotencyKey: input.IdempotencyKey,
	})
}

// ListTools lists host tools visible to the current agent request.
func (c *AgentHostClient) ListTools(ctx context.Context, req *proto.ListAgentToolsRequest) (*proto.ListAgentToolsResponse, error) {
	return c.client.ListTools(ctx, req)
}

// ListToolsForTurn lists host tools using plain Go request fields.
func (c *AgentHostClient) ListToolsForTurn(ctx context.Context, input AgentHostListToolsInput) (*proto.ListAgentToolsResponse, error) {
	return c.ListTools(ctx, &proto.ListAgentToolsRequest{
		SessionId: input.SessionID,
		TurnId:    input.TurnID,
		RunGrant:  input.RunGrant,
		PageSize:  input.PageSize,
		PageToken: input.PageToken,
		Query:     input.Query,
	})
}

// ResolveConnection resolves a configured agent connection for the current turn.
func (c *AgentHostClient) ResolveConnection(ctx context.Context, req *proto.ResolveAgentConnectionRequest) (*proto.ResolvedAgentConnection, error) {
	return c.client.ResolveConnection(ctx, req)
}

// ResolveConnectionForTurn resolves an agent connection using plain Go request
// fields.
func (c *AgentHostClient) ResolveConnectionForTurn(ctx context.Context, input AgentHostResolveConnectionInput) (*proto.ResolvedAgentConnection, error) {
	return c.ResolveConnection(ctx, &proto.ResolveAgentConnectionRequest{
		SessionId:  input.SessionID,
		TurnId:     input.TurnID,
		Connection: input.Connection,
		Instance:   input.Instance,
		RunGrant:   input.RunGrant,
	})
}
