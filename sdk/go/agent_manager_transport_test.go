package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
)

type agentManagerTransportHarness struct {
	proto.UnimplementedAgentProviderServer

	mu              sync.Mutex
	sessionRequests []*proto.CreateAgentProviderSessionRequest
	turnRequests    []*proto.CreateAgentProviderTurnRequest
	tokens          []string
}

func (h *agentManagerTransportHarness) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.sessionRequests = append(h.sessionRequests, gproto.Clone(req).(*proto.CreateAgentProviderSessionRequest))
	h.mu.Unlock()

	return &proto.AgentSession{
		Id:           "session-1",
		ProviderName: req.GetProviderName(),
		Model:        req.GetModel(),
		ClientRef:    req.GetClientRef(),
		State:        proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
	}, nil
}

func (h *agentManagerTransportHarness) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.turnRequests = append(h.turnRequests, gproto.Clone(req).(*proto.CreateAgentProviderTurnRequest))
	h.mu.Unlock()

	return &proto.AgentTurn{
		Id:        "turn-1",
		SessionId: req.GetSessionId(),
		Model:     req.GetModel(),
		Status:    proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING,
	}, nil
}

func TestTransport_AgentManagerTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.AgentManager("parent-token")
	if err != nil {
		t.Fatalf("AgentManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	session, err := client.CreateSession(context.Background(), gestalt.AgentManagerCreateSession{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-1",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ProviderName != "managed" {
		t.Fatalf("provider_name = %q, want %q", session.ProviderName, "managed")
	}
	if session.ID != "session-1" {
		t.Fatalf("session id = %q, want %q", session.ID, "session-1")
	}

	turn, err := client.CreateTurn(context.Background(), gestalt.AgentManagerCreateTurn{
		SessionID: "session-1",
		Model:     "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.ID != "turn-1" {
		t.Fatalf("turn id = %q, want %q", turn.ID, "turn-1")
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.tokens) != 2 || harness.tokens[0] != "relay-token-go" || harness.tokens[1] != "relay-token-go" {
		t.Fatalf("relay tokens = %#v, want [relay-token-go relay-token-go]", harness.tokens)
	}
	if len(harness.sessionRequests) != 1 {
		t.Fatalf("session requests len = %d, want 1", len(harness.sessionRequests))
	}
	if harness.sessionRequests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("session invocation token = %q, want %q", harness.sessionRequests[0].GetInvocationToken(), "parent-token")
	}
	if harness.sessionRequests[0].GetProviderName() != "managed" || harness.sessionRequests[0].GetModel() != "gpt-test" {
		t.Fatalf("session request = %+v, want provider_name=managed model=gpt-test", harness.sessionRequests[0])
	}
	if len(harness.turnRequests) != 1 {
		t.Fatalf("turn requests len = %d, want 1", len(harness.turnRequests))
	}
	if harness.turnRequests[0].GetInvocationToken() != "parent-token" {
		t.Fatalf("turn invocation token = %q, want %q", harness.turnRequests[0].GetInvocationToken(), "parent-token")
	}
	if harness.turnRequests[0].GetSessionId() != "session-1" || harness.turnRequests[0].GetModel() != "gpt-test" {
		t.Fatalf("turn request = %+v, want session_id=session-1 model=gpt-test", harness.turnRequests[0])
	}
	if len(harness.turnRequests[0].GetToolRefs()) != 0 {
		t.Fatalf("turn tool refs len = %d, want 0", len(harness.turnRequests[0].GetToolRefs()))
	}
	if harness.turnRequests[0].GetToolSource() != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_UNSPECIFIED {
		t.Fatalf("turn tool source = %s, want unspecified", harness.turnRequests[0].GetToolSource())
	}
}

func TestTransport_AgentManagerCreateTurnNativeValues(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentManagerTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.AgentManager("parent-token")
	if err != nil {
		t.Fatalf("AgentManager: %v", err)
	}
	defer func() { _ = client.Close() }()

	turn, err := client.CreateTurn(context.Background(), gestalt.AgentManagerCreateTurn{
		SessionID: "session-1",
		Model:     "gpt-test",
		Messages: []gestalt.AgentMessage{{
			Role: "user",
			Text: "Summarize",
			Parts: []gestalt.AgentMessagePart{{
				Text: "Summarize",
			}},
			Metadata: map[string]any{"source": "native"},
		}},
		ToolRefs: []gestalt.AgentToolRef{{
			App: "github",
			Operation:  "issues.get",
			Connection: "default",
		}},
		ToolSource:     gestalt.AgentToolSourceModeMCPCatalog,
		ResponseSchema: map[string]any{"type": "object"},
		Metadata:       map[string]any{"request": "native"},
		ModelOptions:   map[string]any{"temperature": 0},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.ID != "turn-1" {
		t.Fatalf("turn id = %q, want turn-1", turn.ID)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.turnRequests) != 1 {
		t.Fatalf("turn requests len = %d, want 1", len(harness.turnRequests))
	}
	got := harness.turnRequests[0]
	if got.GetInvocationToken() != "parent-token" {
		t.Fatalf("invocation token = %q, want parent-token", got.GetInvocationToken())
	}
	if got.GetMessages()[0].GetParts()[0].GetType() != proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT {
		t.Fatalf("message part type = %s, want text", got.GetMessages()[0].GetParts()[0].GetType())
	}
	if metadata := got.GetMessages()[0].GetMetadata().AsMap(); metadata["source"] != "native" {
		t.Fatalf("message metadata = %#v", metadata)
	}
	if got.GetToolRefs()[0].GetApp() != "github" || got.GetToolRefs()[0].GetConnection() != "default" {
		t.Fatalf("tool refs = %#v", got.GetToolRefs())
	}
	if schema := got.GetResponseSchema().AsMap(); schema["type"] != "object" {
		t.Fatalf("response schema = %#v", schema)
	}
}
