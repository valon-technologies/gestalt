package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type agentTransportHarness struct {
	proto.UnimplementedAgentProviderServer

	mu                 sync.Mutex
	sessionRequests    []*proto.CreateAgentProviderSessionRequest
	turnRequests       []*proto.CreateAgentProviderTurnRequest
	getTurnRequests    []*proto.GetAgentProviderTurnRequest
	cancelTurnRequests []*proto.CancelAgentProviderTurnRequest
	tokens             []string
	requireAuthContext bool
}

func (h *agentTransportHarness) CreateSession(ctx context.Context, req *proto.CreateAgentProviderSessionRequest) (*proto.AgentSession, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.sessionRequests = append(h.sessionRequests, gproto.Clone(req).(*proto.CreateAgentProviderSessionRequest))
	h.mu.Unlock()

	if h.requireAuthContext && req.GetContext() == nil {
		return nil, status.Error(codes.FailedPrecondition, "request context is required")
	}
	return &proto.AgentSession{
		Id:           "session-1",
		ProviderName: req.GetProviderName(),
		Model:        req.GetModel(),
		ClientRef:    req.GetClientRef(),
		State:        proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
	}, nil
}

func (h *agentTransportHarness) CreateTurn(ctx context.Context, req *proto.CreateAgentProviderTurnRequest) (*proto.AgentTurn, error) {
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

func (h *agentTransportHarness) GetTurn(ctx context.Context, req *proto.GetAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.getTurnRequests = append(h.getTurnRequests, gproto.Clone(req).(*proto.GetAgentProviderTurnRequest))
	h.mu.Unlock()

	return &proto.AgentTurn{
		Id:        req.GetTurnId(),
		SessionId: "session-1",
		Status:    proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED,
	}, nil
}

func (h *agentTransportHarness) CancelTurn(ctx context.Context, req *proto.CancelAgentProviderTurnRequest) (*proto.AgentTurn, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.cancelTurnRequests = append(h.cancelTurnRequests, gproto.Clone(req).(*proto.CancelAgentProviderTurnRequest))
	h.mu.Unlock()

	return &proto.AgentTurn{
		Id:        req.GetTurnId(),
		SessionId: "session-1",
		Status:    proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED,
	}, nil
}

func TestTransport_AgentTCPTargetTokenEnv(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewAgent(agentTransportRequest())
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	session, err := client.CreateSession(context.Background(), gestalt.AgentCreateSession{
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

	turn, err := client.CreateTurn(context.Background(), gestalt.AgentCreateTurn{
		SessionID:      "session-1",
		Model:          "gpt-test",
		TimeoutSeconds: 120,
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
	if got := harness.sessionRequests[0].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("session subject = %q, want user:transport", got)
	}
	if harness.sessionRequests[0].GetProviderName() != "managed" || harness.sessionRequests[0].GetModel() != "gpt-test" {
		t.Fatalf("session request = %+v, want provider_name=managed model=gpt-test", harness.sessionRequests[0])
	}
	if len(harness.turnRequests) != 1 {
		t.Fatalf("turn requests len = %d, want 1", len(harness.turnRequests))
	}
	if got := harness.turnRequests[0].GetContext().GetSubject().GetId(); got != "user:transport" {
		t.Fatalf("turn subject = %q, want user:transport", got)
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

func TestTransport_AgentWorkflowContext(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewAgent(agentTransportRequest())
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	workflow := map[string]any{
		"providerName": "indexeddb",
		"runId":        "run-1",
		"runAs": map[string]any{
			"id":                  "service_account:workflow-runner",
			"kind":                "service_account",
			"credentialSubjectId": "service_account:workflow-runner",
		},
	}
	ctx := gestalt.WithWorkflowContext(context.Background(), workflow)
	if _, err := client.CreateSession(ctx, gestalt.AgentCreateSession{
		ProviderName: "managed",
		Model:        "gpt-test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := client.CreateTurn(ctx, gestalt.AgentCreateTurn{
		SessionID:      "session-1",
		Model:          "gpt-test",
		TimeoutSeconds: 120,
	}); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if _, err := client.GetTurn(ctx, gestalt.AgentGetTurn{TurnID: "turn-1"}); err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if _, err := client.CancelTurn(ctx, gestalt.AgentCancelTurn{TurnID: "turn-1", Reason: "test"}); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.sessionRequests) != 1 || len(harness.turnRequests) != 1 || len(harness.getTurnRequests) != 1 || len(harness.cancelTurnRequests) != 1 {
		t.Fatalf("requests = sessions:%d turns:%d get:%d cancel:%d", len(harness.sessionRequests), len(harness.turnRequests), len(harness.getTurnRequests), len(harness.cancelTurnRequests))
	}
	if got := harness.turnRequests[0].GetContext().GetWorkflow().AsMap()["runId"]; got != "run-1" {
		t.Fatalf("workflow runId = %#v, want run-1", got)
	}
	if got := harness.getTurnRequests[0].GetContext().GetWorkflow().AsMap()["runId"]; got != "run-1" {
		t.Fatalf("get workflow runId = %#v, want run-1", got)
	}
	if got := harness.cancelTurnRequests[0].GetContext().GetWorkflow().AsMap()["runId"]; got != "run-1" {
		t.Fatalf("cancel workflow runId = %#v, want run-1", got)
	}
}

func TestTransport_AgentMissingRequestContextFails(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentTransportHarness{requireAuthContext: true}
	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewAgent(gestalt.Request{})
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.CreateSession(context.Background(), gestalt.AgentCreateSession{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CreateSession code = %v, want %v (err=%v)", status.Code(err), codes.FailedPrecondition, err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.sessionRequests) != 1 {
		t.Fatalf("session requests len = %d, want 1", len(harness.sessionRequests))
	}
	if harness.sessionRequests[0].GetContext() != nil {
		t.Fatalf("context = %#v, want nil", harness.sessionRequests[0].GetContext())
	}
}

func TestTransport_AgentCreateTurnNativeValues(t *testing.T) {
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentProviderServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	client, err := gestalt.NewAgent(agentTransportRequest())
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	defer func() { _ = client.Close() }()

	turn, err := client.CreateTurn(context.Background(), gestalt.AgentCreateTurn{
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
			App:        "github",
			Operation:  "issues.get",
			Connection: "default",
		}},
		ToolSource: gestalt.AgentToolSourceModeMCPCatalog,
		Output: &gestalt.AgentOutput{
			Structured: &gestalt.AgentStructuredOutput{Schema: map[string]any{"type": "object"}},
		},
		Metadata:       map[string]any{"request": "native"},
		ModelOptions:   map[string]any{"temperature": 0},
		TimeoutSeconds: 120,
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
	if got.GetContext().GetSubject().GetId() != "user:transport" {
		t.Fatalf("subject = %q, want user:transport", got.GetContext().GetSubject().GetId())
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
	if schema := got.GetOutput().GetStructured().GetSchema().AsMap(); schema["type"] != "object" {
		t.Fatalf("output schema = %#v", schema)
	}
	if got.GetTimeoutSeconds() != 120 {
		t.Fatalf("timeout_seconds = %d, want 120", got.GetTimeoutSeconds())
	}
}

func agentTransportRequest() gestalt.Request {
	return gestalt.Request{
		Subject: gestalt.Subject{
			ID:                  "user:transport",
			CredentialSubjectID: "user:transport",
			Email:               "transport@example.test",
		},
	}
}
