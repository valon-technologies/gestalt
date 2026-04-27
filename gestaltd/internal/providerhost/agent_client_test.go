package providerhost

import (
	"context"
	"errors"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAgentProviderClient struct {
	getCapabilities func(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error)
}

func (c *fakeAgentProviderClient) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, errors.New("unexpected CreateSession call")
}

func (c *fakeAgentProviderClient) GetSession(context.Context, *proto.GetAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, errors.New("unexpected GetSession call")
}

func (c *fakeAgentProviderClient) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderSessionsResponse, error) {
	return nil, errors.New("unexpected ListSessions call")
}

func (c *fakeAgentProviderClient) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, errors.New("unexpected UpdateSession call")
}

func (c *fakeAgentProviderClient) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, errors.New("unexpected CreateTurn call")
}

func (c *fakeAgentProviderClient) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, errors.New("unexpected GetTurn call")
}

func (c *fakeAgentProviderClient) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnsResponse, error) {
	return nil, errors.New("unexpected ListTurns call")
}

func (c *fakeAgentProviderClient) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, errors.New("unexpected CancelTurn call")
}

func (c *fakeAgentProviderClient) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
	return nil, errors.New("unexpected ListTurnEvents call")
}

func (c *fakeAgentProviderClient) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, errors.New("unexpected GetInteraction call")
}

func (c *fakeAgentProviderClient) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderInteractionsResponse, error) {
	return nil, errors.New("unexpected ListInteractions call")
}

func (c *fakeAgentProviderClient) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, errors.New("unexpected ResolveInteraction call")
}

func (c *fakeAgentProviderClient) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest, opts ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
	if c.getCapabilities != nil {
		return c.getCapabilities(ctx, req, opts...)
	}
	return &proto.AgentProviderCapabilities{}, nil
}

func TestRemoteAgentPingRequiresAgentProviderSurface(t *testing.T) {
	t.Parallel()

	agent := &remoteAgent{
		client: &fakeAgentProviderClient{
			getCapabilities: func(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
				return nil, status.Error(codes.Unimplemented, "unknown service")
			},
		},
		runtime: &fakeProviderLifecycleClient{},
	}

	err := agent.Ping(context.Background())
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("Ping error code = %s, want %s (err=%v)", status.Code(err), codes.Unimplemented, err)
	}
}
