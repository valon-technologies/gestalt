package providerhost

import (
	"context"
	"errors"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type fakeAgentProviderClient struct {
	getCapabilities func(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error)
	listTurnEvents  func(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error)
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

func (c *fakeAgentProviderClient) ListTurnEvents(ctx context.Context, req *proto.ListAgentProviderTurnEventsRequest, opts ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
	if c.listTurnEvents != nil {
		return c.listTurnEvents(ctx, req, opts...)
	}
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

func TestRemoteAgentListTurnEventsPreservesDisplay(t *testing.T) {
	t.Parallel()

	input, err := structpb.NewValue(map[string]any{"query": "fixture"})
	if err != nil {
		t.Fatalf("NewValue input: %v", err)
	}
	agent := &remoteAgent{
		client: &fakeAgentProviderClient{
			listTurnEvents: func(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
				return &proto.ListAgentProviderTurnEventsResponse{
					Events: []*proto.AgentTurnEvent{{
						Id:         "event-1",
						TurnId:     "turn-1",
						Seq:        7,
						Type:       "provider.tool",
						Visibility: "public",
						Display: &proto.AgentTurnDisplay{
							Kind:     "tool",
							Phase:    "started",
							Label:    "Lookup fixture",
							Ref:      "call-1",
							Action:   "Running",
							Format:   "json",
							Language: "json",
							Input:    input,
						},
					}},
				}, nil
			},
		},
	}

	events, err := agent.ListTurnEvents(context.Background(), coreagent.ListTurnEventsRequest{TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("ListTurnEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	display := events[0].Display
	if display == nil || display.Kind != "tool" || display.Phase != "started" || display.Label != "Lookup fixture" || display.Ref != "call-1" || display.Action != "Running" || display.Format != "json" || display.Language != "json" {
		t.Fatalf("display = %#v", display)
	}
	inputMap, ok := display.Input.(map[string]any)
	if !ok || inputMap["query"] != "fixture" {
		t.Fatalf("display input = %#v, want query fixture", display.Input)
	}
}

func TestTurnEventsToProtoPreservesEnvelopeWhenDataInvalid(t *testing.T) {
	t.Parallel()

	events := turnEventsToProto([]*coreagent.TurnEvent{{
		ID:         "event-1",
		TurnID:     "turn-1",
		Seq:        1,
		Type:       "tool.started",
		Visibility: "public",
		Data: map[string]any{
			"bad": map[int]string{1: "not a protobuf struct"},
		},
		Display: &coreagent.TurnDisplay{
			Kind:     "tool",
			Phase:    "started",
			Label:    "Lookup fixture",
			Ref:      "call-1",
			Action:   "Running",
			Format:   "json",
			Language: "json",
			Input: map[string]any{
				"query": "fixture",
			},
		},
	}})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].GetData() != nil {
		t.Fatalf("event data = %#v, want omitted invalid data", events[0].GetData())
	}
	display := events[0].GetDisplay()
	if display == nil || display.GetKind() != "tool" || display.GetPhase() != "started" || display.GetRef() != "call-1" || display.GetAction() != "Running" || display.GetFormat() != "json" || display.GetLanguage() != "json" {
		t.Fatalf("display = %#v", display)
	}
	inputMap := display.GetInput().GetStructValue().AsMap()
	if inputMap["query"] != "fixture" {
		t.Fatalf("display input = %#v, want query fixture", inputMap)
	}
}
