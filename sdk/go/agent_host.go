package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type agentHost struct {
	client proto.AgentHostClient
}

// AgentHost is the fakeable contract for host tool calls made by agent providers.
type AgentHostAPI interface {
	Close() error
	ExecuteTool(context.Context, AgentHostExecuteToolInput) (*ExecuteAgentToolResponse, error)
	ListTools(context.Context, AgentHostListToolsInput) (*ListAgentToolsResponse, error)
	ResolveConnection(context.Context, AgentHostResolveConnectionInput) (*ResolvedAgentConnection, error)
}

// AgentHostListToolsInput contains plain fields for listing tools available to
// one agent turn.
type AgentHostListToolsInput struct {
	SessionID string
	TurnID    string
	Context   *proto.RequestContext
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
	IdempotencyKey string
	Context        *proto.RequestContext
}

// AgentHostResolveConnectionInput contains plain fields for resolving a
// configured connection during one agent turn.
type AgentHostResolveConnectionInput struct {
	SessionID  string
	TurnID     string
	Connection string
	Instance   string
	Context    *proto.RequestContext
}

var sharedAgentHostTransport sharedManagerTransport[proto.AgentHostClient]

// AgentHost returns a shared host tool capability for agent providers.
func AgentHost() (AgentHostAPI, error) {
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
	return &agentHost{
		client: client,
	}, nil
}

// Close is a no-op because this capability uses shared transport.
func (c *agentHost) Close() error {
	return nil
}

// ExecuteTool executes a host tool using plain Go request fields.
func (c *agentHost) ExecuteTool(ctx context.Context, input AgentHostExecuteToolInput) (*ExecuteAgentToolResponse, error) {
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
		IdempotencyKey: input.IdempotencyKey,
		Context:        agentHostRequestContext(ctx, input.Context),
	})
	if err != nil {
		return nil, err
	}
	return executeAgentToolResponseFromProto(resp), nil
}

// ListTools lists host tools visible to the current agent request using plain
// Go request fields.
func (c *agentHost) ListTools(ctx context.Context, input AgentHostListToolsInput) (*ListAgentToolsResponse, error) {
	resp, err := c.client.ListTools(ctx, &proto.ListAgentToolsRequest{
		SessionId: input.SessionID,
		TurnId:    input.TurnID,
		PageSize:  input.PageSize,
		PageToken: input.PageToken,
		Query:     input.Query,
		Context:   agentHostRequestContext(ctx, input.Context),
	})
	if err != nil {
		return nil, err
	}
	return listAgentToolsResponseFromProto(resp), nil
}

// ResolveConnection resolves a configured agent connection for the current turn
// using plain Go request fields.
func (c *agentHost) ResolveConnection(ctx context.Context, input AgentHostResolveConnectionInput) (*ResolvedAgentConnection, error) {
	resp, err := c.client.ResolveConnection(ctx, &proto.ResolveAgentConnectionRequest{
		SessionId:  input.SessionID,
		TurnId:     input.TurnID,
		Connection: input.Connection,
		Instance:   input.Instance,
		Context:    agentHostRequestContext(ctx, input.Context),
	})
	if err != nil {
		return nil, err
	}
	return resolvedAgentConnectionFromProto(resp), nil
}

func agentHostRequestContext(ctx context.Context, explicit *proto.RequestContext) *proto.RequestContext {
	if explicit != nil {
		return cloneRequestContext(explicit)
	}
	return requestContextFromContext(ctx)
}
