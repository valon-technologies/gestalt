package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// AgentHostClient calls host tool APIs from an agent provider.
type AgentHostClient struct {
	client proto.AgentHostClient
}

// AgentHostClientContract is the fakeable client contract for host tool calls
// made by agent providers.
type AgentHostClientContract interface {
	Close() error
	ExecuteTool(context.Context, AgentHostExecuteToolInput) (*ExecuteAgentToolResponse, error)
	ExecuteToolForTurn(context.Context, AgentHostExecuteToolInput) (*ExecuteAgentToolResponse, error)
	ListTools(context.Context, AgentHostListToolsInput) (*ListAgentToolsResponse, error)
	ListToolsForTurn(context.Context, AgentHostListToolsInput) (*ListAgentToolsResponse, error)
	ResolveConnection(context.Context, AgentHostResolveConnectionInput) (*ResolvedAgentConnection, error)
	ResolveConnectionForTurn(context.Context, AgentHostResolveConnectionInput) (*ResolvedAgentConnection, error)
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
	target, token, err := hostServiceTarget("agent host")
	if err != nil {
		return nil, err
	}

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

// ExecuteTool executes a host tool using plain Go request fields.
func (c *AgentHostClient) ExecuteTool(ctx context.Context, input AgentHostExecuteToolInput) (*ExecuteAgentToolResponse, error) {
	var arguments *structpb.Struct
	if input.Arguments != nil {
		var err error
		arguments, err = structFromAny(input.Arguments)
		if err != nil {
			return nil, err
		}
	}
	resp, err := c.client.ExecuteTool(ctx, &proto.ExecuteAgentToolRequest{
		SessionId:      input.SessionID,
		TurnId:         input.TurnID,
		ToolCallId:     input.ToolCallID,
		ToolId:         input.ToolID,
		Arguments:      arguments,
		RunGrant:       input.RunGrant,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return executeAgentToolResponseFromProto(resp), nil
}

// ExecuteToolForTurn executes a host tool using plain Go request fields.
func (c *AgentHostClient) ExecuteToolForTurn(ctx context.Context, input AgentHostExecuteToolInput) (*ExecuteAgentToolResponse, error) {
	return c.ExecuteTool(ctx, input)
}

// ListTools lists host tools visible to the current agent request using plain
// Go request fields.
func (c *AgentHostClient) ListTools(ctx context.Context, input AgentHostListToolsInput) (*ListAgentToolsResponse, error) {
	resp, err := c.client.ListTools(ctx, &proto.ListAgentToolsRequest{
		SessionId: input.SessionID,
		TurnId:    input.TurnID,
		RunGrant:  input.RunGrant,
		PageSize:  input.PageSize,
		PageToken: input.PageToken,
		Query:     input.Query,
	})
	if err != nil {
		return nil, err
	}
	return listAgentToolsResponseFromProto(resp), nil
}

// ListToolsForTurn lists host tools using plain Go request fields.
func (c *AgentHostClient) ListToolsForTurn(ctx context.Context, input AgentHostListToolsInput) (*ListAgentToolsResponse, error) {
	return c.ListTools(ctx, input)
}

// ResolveConnection resolves a configured agent connection for the current turn
// using plain Go request fields.
func (c *AgentHostClient) ResolveConnection(ctx context.Context, input AgentHostResolveConnectionInput) (*ResolvedAgentConnection, error) {
	resp, err := c.client.ResolveConnection(ctx, &proto.ResolveAgentConnectionRequest{
		SessionId:  input.SessionID,
		TurnId:     input.TurnID,
		Connection: input.Connection,
		Instance:   input.Instance,
		RunGrant:   input.RunGrant,
	})
	if err != nil {
		return nil, err
	}
	return resolvedAgentConnectionFromProto(resp), nil
}

// ResolveConnectionForTurn resolves an agent connection using plain Go request
// fields.
func (c *AgentHostClient) ResolveConnectionForTurn(ctx context.Context, input AgentHostResolveConnectionInput) (*ResolvedAgentConnection, error) {
	return c.ResolveConnection(ctx, input)
}
