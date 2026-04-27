package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

const EnvAgentHostSocket = "GESTALT_AGENT_HOST_SOCKET"
const EnvAgentHostSocketToken = EnvAgentHostSocket + "_TOKEN"

type AgentHostClient struct {
	client proto.AgentHostClient
}

var sharedAgentHostTransport sharedManagerTransport[proto.AgentHostClient]

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

func (c *AgentHostClient) Close() error {
	return nil
}

func (c *AgentHostClient) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	return c.client.ExecuteTool(ctx, req)
}

func (c *AgentHostClient) SearchTools(ctx context.Context, req *proto.SearchAgentToolsRequest) (*proto.SearchAgentToolsResponse, error) {
	return c.client.SearchTools(ctx, req)
}
