package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const EnvAgentHostSocket = "GESTALT_AGENT_HOST_SOCKET"

type AgentHostClient struct {
	client proto.AgentHostClient
	conn   *grpc.ClientConn
}

func AgentHost() (*AgentHostClient, error) {
	socketPath := os.Getenv(EnvAgentHostSocket)
	if socketPath == "" {
		return nil, fmt.Errorf("agent host: %s is not set", EnvAgentHostSocket)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("agent host: connect to host: %w", err)
	}
	return &AgentHostClient{
		client: proto.NewAgentHostClient(conn),
		conn:   conn,
	}, nil
}

func (c *AgentHostClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *AgentHostClient) ExecuteTool(ctx context.Context, req *proto.ExecuteAgentToolRequest) (*proto.ExecuteAgentToolResponse, error) {
	return c.client.ExecuteTool(ctx, req)
}
