package agents

import (
	"context"
	"errors"
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeGestaltAgentClient struct {
	createSession func(context.Context, *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error)
	getTurn       func(context.Context, *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error)
	capabilities  func(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*proto.AgentProviderCapabilities, error)
}

func (f *fakeGestaltAgentClient) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest, _ ...grpc.CallOption) (*proto.AgentSession, error) {
	if f.createSession != nil {
		return f.createSession(ctx, req)
	}
	return nil, errors.New("unexpected CreateSession")
}

func (f *fakeGestaltAgentClient) GetSession(context.Context, *proto.GetAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, errors.New("unexpected GetSession")
}

func (f *fakeGestaltAgentClient) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderSessionsResponse, error) {
	return nil, errors.New("unexpected ListSessions")
}

func (f *fakeGestaltAgentClient) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, errors.New("unexpected UpdateSession")
}

func (f *fakeGestaltAgentClient) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, errors.New("unexpected CreateTurn")
}

func (f *fakeGestaltAgentClient) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest, _ ...grpc.CallOption) (*proto.AgentTurn, error) {
	if f.getTurn != nil {
		return f.getTurn(ctx, req)
	}
	return nil, errors.New("unexpected GetTurn")
}

func (f *fakeGestaltAgentClient) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnsResponse, error) {
	return nil, errors.New("unexpected ListTurns")
}

func (f *fakeGestaltAgentClient) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, errors.New("unexpected CancelTurn")
}

func (f *fakeGestaltAgentClient) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
	return nil, errors.New("unexpected ListTurnEvents")
}

func (f *fakeGestaltAgentClient) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, errors.New("unexpected GetInteraction")
}

func (f *fakeGestaltAgentClient) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderInteractionsResponse, error) {
	return nil, errors.New("unexpected ListInteractions")
}

func (f *fakeGestaltAgentClient) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, errors.New("unexpected ResolveInteraction")
}

func (f *fakeGestaltAgentClient) GetCapabilities(ctx context.Context, req *proto.GetAgentProviderCapabilitiesRequest, _ ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
	if f.capabilities != nil {
		return f.capabilities(ctx, req)
	}
	return &proto.AgentProviderCapabilities{}, nil
}

func TestGestaltRemoteAgentSetsProviderName(t *testing.T) {
	t.Parallel()

	var gotProvider string
	client := &fakeGestaltAgentClient{
		createSession: func(_ context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
			gotProvider = req.GetProviderName()
			return &proto.AgentSession{Id: "session-1", ProviderName: "managed"}, nil
		},
	}
	provider := NewGestaltRemoteProvider(client, "managed")
	session, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if gotProvider != "managed" {
		t.Fatalf("provider_name = %q, want managed", gotProvider)
	}
	if session.ID != "session-1" {
		t.Fatalf("session id = %q", session.ID)
	}
}

func TestGestaltRemoteAgentMapsAuthErrors(t *testing.T) {
	t.Parallel()

	client := &fakeGestaltAgentClient{
		getTurn: func(context.Context, *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
			return nil, status.Error(codes.PermissionDenied, "denied")
		},
	}
	provider := NewGestaltRemoteProvider(client, "managed")
	_, err := provider.GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if !errors.Is(err, invocation.ErrAuthorizationDenied) {
		t.Fatalf("GetTurn err = %v, want ErrAuthorizationDenied", err)
	}
}

var _ coreagent.Provider = NewGestaltRemoteProvider(&fakeGestaltAgentClient{}, "managed")
