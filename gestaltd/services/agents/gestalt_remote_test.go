package agents

import (
	"context"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type fakeGestaltAgentClient struct {
	createSession func(context.Context, *proto.CreateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error)
	getTurn       func(context.Context, *proto.GetAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error)
}

func (f *fakeGestaltAgentClient) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest, opts ...grpc.CallOption) (*proto.AgentSession, error) {
	if f.createSession != nil {
		return f.createSession(ctx, req, opts...)
	}
	return &proto.AgentSession{Id: "session-1"}, nil
}

func (f *fakeGestaltAgentClient) GetSession(context.Context, *proto.GetAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, nil
}

func (f *fakeGestaltAgentClient) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderSessionsResponse, error) {
	return &proto.ListAgentProviderSessionsResponse{}, nil
}

func (f *fakeGestaltAgentClient) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, nil
}

func (f *fakeGestaltAgentClient) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return &proto.AgentTurn{Id: "turn-1"}, nil
}

func (f *fakeGestaltAgentClient) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest, opts ...grpc.CallOption) (*proto.AgentTurn, error) {
	if f.getTurn != nil {
		return f.getTurn(ctx, req, opts...)
	}
	return &proto.AgentTurn{Id: req.GetTurnId()}, nil
}

func (f *fakeGestaltAgentClient) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnsResponse, error) {
	return &proto.ListAgentProviderTurnsResponse{}, nil
}

func (f *fakeGestaltAgentClient) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, nil
}

func (f *fakeGestaltAgentClient) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
	return &proto.ListAgentProviderTurnEventsResponse{}, nil
}

func (f *fakeGestaltAgentClient) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, nil
}

func (f *fakeGestaltAgentClient) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderInteractionsResponse, error) {
	return &proto.ListAgentProviderInteractionsResponse{}, nil
}

func (f *fakeGestaltAgentClient) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, nil
}

func (f *fakeGestaltAgentClient) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
	return &proto.AgentProviderCapabilities{}, nil
}

func TestGestaltRemoteAgentSetsProviderName(t *testing.T) {
	t.Parallel()

	var gotProvider string
	client := &fakeGestaltAgentClient{
		createSession: func(_ context.Context, req *proto.CreateAgentProviderSessionRequest, _ ...grpc.CallOption) (*proto.AgentSession, error) {
			gotProvider = req.GetProviderName()
			return &proto.AgentSession{Id: "session-1"}, nil
		},
	}
	provider, err := NewGestaltRemoteProvider("managed", client)
	if err != nil {
		t.Fatalf("NewGestaltRemoteProvider: %v", err)
	}
	if _, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if gotProvider != "managed" {
		t.Fatalf("provider_name = %q, want managed", gotProvider)
	}
}

func TestGestaltRemoteAgentGetTurnLifecycle(t *testing.T) {
	t.Parallel()

	client := &fakeGestaltAgentClient{
		getTurn: func(_ context.Context, req *proto.GetAgentProviderTurnRequest, _ ...grpc.CallOption) (*proto.AgentTurn, error) {
			if req.GetProviderName() != "managed" || req.GetTurnId() != "turn-42" {
				t.Fatalf("request = %#v", req)
			}
			return &proto.AgentTurn{Id: "turn-42"}, nil
		},
	}
	provider, err := NewGestaltRemoteProvider("managed", client)
	if err != nil {
		t.Fatalf("NewGestaltRemoteProvider: %v", err)
	}
	turn, err := provider.GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-42"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if turn.ID != "turn-42" {
		t.Fatalf("turn id = %q", turn.ID)
	}
}
