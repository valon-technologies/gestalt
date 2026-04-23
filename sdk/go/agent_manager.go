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

func (c *AgentManagerClient) Run(ctx context.Context, req *proto.AgentManagerRunRequest) (*proto.ManagedAgentRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.Run(ctx, value)
}

func (c *AgentManagerClient) GetRun(ctx context.Context, req *proto.AgentManagerGetRunRequest) (*proto.ManagedAgentRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerGetRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerGetRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.GetRun(ctx, value)
}

func (c *AgentManagerClient) ListRuns(ctx context.Context, req *proto.AgentManagerListRunsRequest) (*proto.AgentManagerListRunsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerListRunsRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerListRunsRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.ListRuns(ctx, value)
}

func (c *AgentManagerClient) CancelRun(ctx context.Context, req *proto.AgentManagerCancelRunRequest) (*proto.ManagedAgentRun, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("agent manager: client is not initialized")
	}
	value := &proto.AgentManagerCancelRunRequest{}
	if req != nil {
		value = gproto.Clone(req).(*proto.AgentManagerCancelRunRequest)
	}
	value.InvocationToken = c.invocationToken
	return c.client.CancelRun(ctx, value)
}
