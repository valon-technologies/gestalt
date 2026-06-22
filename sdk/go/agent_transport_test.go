package gestalt_test

import (
	"context"
	"net"
	"sync"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/client"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type agentTransportHarness struct {
	proto.UnimplementedAgentServer

	mu                 sync.Mutex
	sessionRequests    []*proto.CreateAgentProviderSessionRequest
	turnRequests       []*proto.CreateAgentProviderTurnRequest
	getTurnRequests    []*proto.GetAgentProviderTurnRequest
	cancelTurnRequests []*proto.CancelAgentProviderTurnRequest
	listSessionsRequests []*proto.ListAgentProviderSessionsRequest
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

func (h *agentTransportHarness) ListSessions(ctx context.Context, req *proto.ListAgentProviderSessionsRequest) (*proto.ListAgentProviderSessionsResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	h.mu.Lock()
	if values := md.Get("x-gestalt-host-service-relay-token"); len(values) > 0 {
		h.tokens = append(h.tokens, values...)
	}
	h.listSessionsRequests = append(h.listSessionsRequests, gproto.Clone(req).(*proto.ListAgentProviderSessionsRequest))
	h.mu.Unlock()

	return &proto.ListAgentProviderSessionsResponse{
		Sessions: []*proto.AgentSession{{
			Id:           "session-listed",
			ProviderName: req.GetProviderName(),
			Model:        "gpt-test",
			ClientRef:    "cli-session-listed",
			State:        proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
		}},
	}, nil
}

func startAgentTransportHarness(t *testing.T) (*agentTransportHarness, string) {
	t.Helper()
	address := reserveTCPAddress()
	lis, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	harness := &agentTransportHarness{}
	srv := grpc.NewServer()
	proto.RegisterAgentServer(srv, harness)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)
	return harness, address
}

func agentTransportRequestContext() *client.RequestContext {
	return &client.RequestContext{
		Subject: &client.SubjectContext{
			Id:                  "user:transport",
						Email:               "transport@example.test",
		},
	}
}

func TestTransport_AgentTCPTargetTokenEnv(t *testing.T) {
	harness, address := startAgentTransportHarness(t)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	agent, err := client.ConnectAgent(context.Background(), "", client.WithRequestContext(agentTransportRequestContext()))
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}

	session, err := agent.CreateSessionRaw(context.Background(), &client.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		ClientRef:    "cli-session-1",
		Tools: &client.AgentToolConfig{Source: &client.AgentToolConfigSourceCatalog{Value: &client.AgentCatalogToolConfig{
			Refs: []*client.AgentToolRef{{
				App:       "slack",
				Operation: "chat.postMessage",
			}},
			Tools: []*client.ListedAgentTool{{
				Id:          "tool-slack",
				McpName:     "slack__chat_post_message",
				Title:       "Send Slack message",
				Description: "Post a Slack message",
				InputSchema: `{"type":"object"}`,
				Ref: &client.AgentToolRef{
					App:       "slack",
					Operation: "chat.postMessage",
				},
			}},
		}}},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ProviderName != "managed" {
		t.Fatalf("provider_name = %q, want %q", session.ProviderName, "managed")
	}
	if session.Id != "session-1" {
		t.Fatalf("session id = %q, want %q", session.Id, "session-1")
	}

	turn, err := agent.CreateTurnRaw(context.Background(), &client.CreateAgentProviderTurnRequest{
		SessionId:      "session-1",
		Model:          "gpt-test",
		TimeoutSeconds: 120,
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.Id != "turn-1" {
		t.Fatalf("turn id = %q, want %q", turn.Id, "turn-1")
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
	sessionCatalog := harness.sessionRequests[0].GetTools().GetCatalog()
	if sessionCatalog == nil {
		t.Fatalf("session tools = %#v, want catalog", harness.sessionRequests[0].GetTools())
	}
	if got := sessionCatalog.GetRefs()[0].GetOperation(); got != "chat.postMessage" {
		t.Fatalf("session tool ref operation = %q, want chat.postMessage", got)
	}
	if got := sessionCatalog.GetTools()[0].GetMcpName(); got != "slack__chat_post_message" {
		t.Fatalf("session listed tool mcp name = %q, want slack__chat_post_message", got)
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
}

func TestTransport_AgentWorkflowContext(t *testing.T) {
	harness, address := startAgentTransportHarness(t)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	workflow := map[string]any{
		"providerName": "indexeddb",
		"runId":        "run-1",
		"runAs": map[string]any{
			"id":   "service_account:workflow-runner",
			"kind": "service_account",
		},
	}
	requestContext := agentTransportRequestContext()
	requestContext.Workflow = workflow
	agent, err := client.ConnectAgent(context.Background(), "", client.WithRequestContext(requestContext))
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}

	ctx := context.Background()
	if _, err := agent.CreateSessionRaw(ctx, &client.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := agent.CreateTurnRaw(ctx, &client.CreateAgentProviderTurnRequest{
		SessionId:      "session-1",
		Model:          "gpt-test",
		TimeoutSeconds: 120,
	}); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if _, err := agent.GetTurn(ctx, "turn-1", nil); err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if _, err := agent.CancelTurn(ctx, "turn-1", &client.AgentCancelTurnOptions{Reason: "test"}); err != nil {
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

func TestTransport_AgentCreateSessionNilTools(t *testing.T) {
	harness, address := startAgentTransportHarness(t)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)

	agent, err := client.ConnectAgent(context.Background(), "", client.WithRequestContext(agentTransportRequestContext()))
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}

	if _, err := agent.CreateSessionRaw(context.Background(), &client.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
		Tools:        nil,
	}); err != nil {
		t.Fatalf("CreateSession nil tools: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.sessionRequests) != 1 {
		t.Fatalf("session requests len = %d, want 1", len(harness.sessionRequests))
	}
	if harness.sessionRequests[0].GetTools() != nil {
		t.Fatalf("session tools = %#v, want nil for nil tools", harness.sessionRequests[0].GetTools())
	}
}

func TestTransport_AgentCallerAndWorkflowContext(t *testing.T) {
	harness, address := startAgentTransportHarness(t)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	agent, err := client.ConnectAgent(context.Background(), "", client.WithRequestContext(&client.RequestContext{
		Subject: &client.SubjectContext{
			Id:                  "service_account:workflow-runner",
					},
		Caller: &client.ProviderContext{Kind: "workflow", Name: "temporal"},
		Workflow: map[string]any{
			"providerName":  "temporal",
			"runId":         "run-1",
			"currentStepId": "review",
		},
	}))
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}

	if _, err := agent.CreateSessionRaw(context.Background(), &client.CreateAgentProviderSessionRequest{
		ProviderName: "claude",
		Model:        "default",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := agent.GetTurn(context.Background(), "turn-1", nil); err != nil {
		t.Fatalf("GetTurn: %v", err)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.sessionRequests) != 1 || len(harness.getTurnRequests) != 1 {
		t.Fatalf("requests = sessions:%d get:%d", len(harness.sessionRequests), len(harness.getTurnRequests))
	}
	got := harness.sessionRequests[0].GetContext()
	if got.GetCaller().GetKind() != "workflow" || got.GetCaller().GetName() != "temporal" {
		t.Fatalf("caller = %#v, want workflow temporal", got.GetCaller())
	}
	if got.GetWorkflow().AsMap()["currentStepId"] != "review" {
		t.Fatalf("workflow context = %#v, want currentStepId=review", got.GetWorkflow().AsMap())
	}
	if got := harness.getTurnRequests[0].GetContext().GetWorkflow().AsMap()["currentStepId"]; got != "review" {
		t.Fatalf("get workflow currentStepId = %#v, want review", got)
	}
}

func TestTransport_AgentCreateTurnNativeValues(t *testing.T) {
	harness, address := startAgentTransportHarness(t)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	agent, err := client.ConnectAgent(context.Background(), "", client.WithRequestContext(agentTransportRequestContext()))
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}

	session, err := agent.CreateSessionRaw(context.Background(), &client.CreateAgentProviderSessionRequest{
		ProviderName: "alpha",
		Model:        "gpt-test",
		Tools: &client.AgentToolConfig{Source: &client.AgentToolConfigSourceCatalog{Value: &client.AgentCatalogToolConfig{
			Refs: []*client.AgentToolRef{{
				App:        "github",
				Operation:  "issues.get",
				Connection: "default",
			}},
		}}},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	turn, err := agent.CreateTurnRaw(context.Background(), &client.CreateAgentProviderTurnRequest{
		SessionId: session.Id,
		Model:     "gpt-test",
		Messages: []*client.AgentMessage{{
			Role: "user",
			Text: "Summarize",
			Parts: []*client.AgentMessagePart{{
				Type: client.AgentMessagePartTypeText,
				Text: "Summarize",
			}},
			Metadata: map[string]any{"source": "native"},
		}},
		Output: &client.AgentOutput{
			Kind: &client.AgentOutputKindStructured{Value: &client.AgentStructuredOutput{Schema: map[string]any{"type": "object"}}},
		},
		Metadata:       map[string]any{"request": "native"},
		ModelOptions:   map[string]any{"temperature": 0},
		TimeoutSeconds: 120,
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.Id != "turn-1" {
		t.Fatalf("turn id = %q, want turn-1", turn.Id)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.sessionRequests) != 1 {
		t.Fatalf("session requests len = %d, want 1", len(harness.sessionRequests))
	}
	if got := harness.sessionRequests[0].GetTools().GetCatalog().GetRefs(); len(got) != 1 || got[0].GetApp() != "github" || got[0].GetConnection() != "default" {
		t.Fatalf("session tool refs = %#v", got)
	}
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
	if schema := got.GetOutput().GetStructured().GetSchema().AsMap(); schema["type"] != "object" {
		t.Fatalf("output schema = %#v", schema)
	}
	if got.GetTimeoutSeconds() != 120 {
		t.Fatalf("timeout_seconds = %d, want 120", got.GetTimeoutSeconds())
	}
}

func TestTransport_AgentListSessionsOptionsOnly(t *testing.T) {
	harness, address := startAgentTransportHarness(t)

	t.Setenv(gestalt.EnvHostServiceSocket, "tcp://"+address)
	t.Setenv(gestalt.EnvHostServiceToken, "relay-token-go")

	agent, err := client.ConnectAgent(context.Background(), "", client.WithRequestContext(agentTransportRequestContext()))
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}

	sessions, err := agent.ListSessions(context.Background(), &client.AgentListSessionsOptions{
		Limit:        10,
		ProviderName: "managed",
		SummaryOnly:  true,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}
	if sessions[0].Id != "session-listed" {
		t.Fatalf("session id = %q, want session-listed", sessions[0].Id)
	}
	if sessions[0].ProviderName != "managed" {
		t.Fatalf("provider_name = %q, want managed", sessions[0].ProviderName)
	}

	harness.mu.Lock()
	defer harness.mu.Unlock()
	if len(harness.listSessionsRequests) != 1 {
		t.Fatalf("list sessions requests len = %d, want 1", len(harness.listSessionsRequests))
	}
	got := harness.listSessionsRequests[0]
	if got.GetLimit() != 10 {
		t.Fatalf("limit = %d, want 10", got.GetLimit())
	}
	if got.GetProviderName() != "managed" {
		t.Fatalf("provider_name = %q, want managed", got.GetProviderName())
	}
	if !got.GetSummaryOnly() {
		t.Fatal("summary_only = false, want true")
	}
	if subject := got.GetContext().GetSubject().GetId(); subject != "user:transport" {
		t.Fatalf("subject = %q, want user:transport", subject)
	}
}
