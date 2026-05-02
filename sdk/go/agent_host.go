package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
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

// ListTools lists host tools visible to the current agent request.
func (c *AgentHostClient) ListTools(ctx context.Context, req *proto.ListAgentToolsRequest) (*proto.ListAgentToolsResponse, error) {
	return c.client.ListTools(ctx, req)
}
