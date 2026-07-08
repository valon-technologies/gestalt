package agents

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type recordingAgentClient struct {
	proto.AgentClient
	lastCreate *proto.CreateAgentProviderSessionRequest
}

func (c *recordingAgentClient) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
	return &proto.AgentProviderCapabilities{}, nil
}

func (c *recordingAgentClient) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest, _ ...grpc.CallOption) (*proto.AgentSession, error) {
	c.lastCreate = req
	return &proto.AgentSession{Id: "session-1", ProviderName: req.GetProviderName()}, nil
}

func TestNewPublicRemoteSetsProviderName(t *testing.T) {
	t.Parallel()

	client := &recordingAgentClient{}
	provider, err := NewPublicRemote(client, "managed")
	if err != nil {
		t.Fatalf("NewPublicRemote: %v", err)
	}
	if _, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if client.lastCreate == nil || client.lastCreate.GetProviderName() != "managed" {
		t.Fatalf("provider_name = %q, want managed", client.lastCreate.GetProviderName())
	}
}

func TestNewPublicRemotePingWithoutLifecycle(t *testing.T) {
	t.Parallel()

	client := &recordingAgentClient{}
	provider, err := NewPublicRemote(client, "managed")
	if err != nil {
		t.Fatalf("NewPublicRemote: %v", err)
	}
	if err := provider.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
