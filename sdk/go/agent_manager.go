package gestalt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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
	return managerUnary(ctx, "agent manager", c != nil && c.client != nil, req, &proto.AgentManagerRunRequest{}, c.invocationToken,
		func(value *proto.AgentManagerRunRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.AgentManagerRunRequest) (*proto.ManagedAgentRun, error) {
			return c.client.Run(ctx, value)
		},
	)
}

func (c *AgentManagerClient) GetRun(ctx context.Context, req *proto.AgentManagerGetRunRequest) (*proto.ManagedAgentRun, error) {
	return managerUnary(ctx, "agent manager", c != nil && c.client != nil, req, &proto.AgentManagerGetRunRequest{}, c.invocationToken,
		func(value *proto.AgentManagerGetRunRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.AgentManagerGetRunRequest) (*proto.ManagedAgentRun, error) {
			return c.client.GetRun(ctx, value)
		},
	)
}

func (c *AgentManagerClient) ListRuns(ctx context.Context, req *proto.AgentManagerListRunsRequest) (*proto.AgentManagerListRunsResponse, error) {
	return managerUnary(ctx, "agent manager", c != nil && c.client != nil, req, &proto.AgentManagerListRunsRequest{}, c.invocationToken,
		func(value *proto.AgentManagerListRunsRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.AgentManagerListRunsRequest) (*proto.AgentManagerListRunsResponse, error) {
			return c.client.ListRuns(ctx, value)
		},
	)
}

func (c *AgentManagerClient) CancelRun(ctx context.Context, req *proto.AgentManagerCancelRunRequest) (*proto.ManagedAgentRun, error) {
	return managerUnary(ctx, "agent manager", c != nil && c.client != nil, req, &proto.AgentManagerCancelRunRequest{}, c.invocationToken,
		func(value *proto.AgentManagerCancelRunRequest, token string) { value.InvocationToken = token },
		func(ctx context.Context, value *proto.AgentManagerCancelRunRequest) (*proto.ManagedAgentRun, error) {
			return c.client.CancelRun(ctx, value)
		},
	)
}
