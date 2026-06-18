package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

type fullAgentProvider struct {
	gestalt.UnimplementedAgentProvider
	closeTracker
	configuredName         string
	receivedSessionRequest *gestalt.CreateAgentProviderSessionRequest
	receivedTurnRequest    *gestalt.CreateAgentProviderTurnRequest
}

func (p *fullAgentProvider) Configure(_ context.Context, name string, _ map[string]any) error {
	p.configuredName = name
	return nil
}

func (p *fullAgentProvider) Metadata() gestalt.ProviderMetadata {
	return gestalt.ProviderMetadata{
		Kind:        gestalt.ProviderKindAgent,
		Name:        "stub-agent",
		DisplayName: "Stub Agent",
		Version:     "1.0",
	}
}

func (p *fullAgentProvider) CreateSession(_ context.Context, req *gestalt.CreateAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	p.receivedSessionRequest = req
	return &gestalt.AgentSession{
		ID:                 "session-1",
		ProviderName:       p.configuredName,
		Model:              req.Model,
		ClientRef:          req.ClientRef,
		State:              gestalt.AgentSessionStateActive,
		Metadata:           req.Metadata,
		CreatedBySubjectID: req.CreatedBySubjectID,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}, nil
}

func (p *fullAgentProvider) GetSession(_ context.Context, req *gestalt.GetAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	return &gestalt.AgentSession{
		ID:           req.SessionID,
		ProviderName: p.configuredName,
		Model:        "gpt-5.1",
		ClientRef:    "client-session-1",
		State:        gestalt.AgentSessionStateArchived,
		Metadata:     map[string]any{"source": "go-test"},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		LastTurnAt:   timePtr(time.Now()),
	}, nil
}

func (p *fullAgentProvider) ListSessions(context.Context, *gestalt.ListAgentProviderSessionsRequest) (*gestalt.ListAgentProviderSessionsResponse, error) {
	return &gestalt.ListAgentProviderSessionsResponse{
		Sessions: []gestalt.AgentSession{{
			ID:           "session-1",
			ProviderName: p.configuredName,
			Model:        "gpt-5.1",
			ClientRef:    "client-session-1",
			State:        gestalt.AgentSessionStateActive,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}},
	}, nil
}

func (p *fullAgentProvider) UpdateSession(_ context.Context, req *gestalt.UpdateAgentProviderSessionRequest) (*gestalt.AgentSession, error) {
	return &gestalt.AgentSession{
		ID:           req.SessionID,
		ProviderName: p.configuredName,
		Model:        "gpt-5.1",
		ClientRef:    req.ClientRef,
		State:        req.State,
		Metadata:     req.Metadata,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}

func (p *fullAgentProvider) CreateTurn(_ context.Context, req *gestalt.CreateAgentProviderTurnRequest) (*gestalt.AgentTurn, error) {
	p.receivedTurnRequest = req
	output := &gestalt.AgentTurnOutput{Text: &gestalt.AgentTurnTextOutput{Text: "echo:Plan it"}}
	if req.Output != nil && req.Output.Structured != nil {
		output = &gestalt.AgentTurnOutput{
			Structured: &gestalt.AgentTurnStructuredOutput{
				Text:  "echo:Plan it",
				Value: req.Output.Structured.Schema,
			},
		}
	}
	return &gestalt.AgentTurn{
		ID:                 req.TurnID,
		SessionID:          req.SessionID,
		ProviderName:       p.configuredName,
		Model:              req.Model,
		Status:             gestalt.AgentExecutionStatusWaitingForInput,
		Messages:           req.Messages,
		Output:             output,
		StatusMessage:      "waiting for input",
		CreatedBySubjectID: req.CreatedBySubjectID,
		CreatedAt:          time.Now(),
		StartedAt:          timePtr(time.Now()),
		ExecutionRef:       req.ExecutionRef,
	}, nil
}

func (p *fullAgentProvider) GetTurn(_ context.Context, req *gestalt.GetAgentProviderTurnRequest) (*gestalt.AgentTurn, error) {
	return &gestalt.AgentTurn{
		ID:            req.TurnID,
		SessionID:     "session-1",
		ProviderName:  p.configuredName,
		Model:         "gpt-5.1",
		Status:        gestalt.AgentExecutionStatusRunning,
		Output:        &gestalt.AgentTurnOutput{Text: &gestalt.AgentTurnTextOutput{Text: "echo:Plan it"}},
		StatusMessage: "running",
		CreatedAt:     time.Now(),
		StartedAt:     timePtr(time.Now()),
	}, nil
}

func (p *fullAgentProvider) ListTurns(_ context.Context, req *gestalt.ListAgentProviderTurnsRequest) (*gestalt.ListAgentProviderTurnsResponse, error) {
	return &gestalt.ListAgentProviderTurnsResponse{
		Turns: []gestalt.AgentTurn{{
			ID:            "turn-1",
			SessionID:     req.SessionID,
			ProviderName:  p.configuredName,
			Model:         "gpt-5.1",
			Status:        gestalt.AgentExecutionStatusSucceeded,
			StatusMessage: "done",
			CreatedAt:     time.Now(),
			StartedAt:     timePtr(time.Now()),
			CompletedAt:   timePtr(time.Now()),
		}},
	}, nil
}

func (p *fullAgentProvider) CancelTurn(_ context.Context, req *gestalt.CancelAgentProviderTurnRequest) (*gestalt.AgentTurn, error) {
	return &gestalt.AgentTurn{
		ID:            req.TurnID,
		SessionID:     "session-1",
		ProviderName:  p.configuredName,
		Model:         "gpt-5.1",
		Status:        gestalt.AgentExecutionStatusCanceled,
		StatusMessage: req.Reason,
		CreatedAt:     time.Now(),
		StartedAt:     timePtr(time.Now()),
		CompletedAt:   timePtr(time.Now()),
	}, nil
}

func (p *fullAgentProvider) ListTurnEvents(_ context.Context, req *gestalt.ListAgentProviderTurnEventsRequest) (*gestalt.ListAgentProviderTurnEventsResponse, error) {
	return &gestalt.ListAgentProviderTurnEventsResponse{
		Events: []gestalt.AgentTurnEvent{{
			ID:         req.TurnID + "-event-1",
			TurnID:     req.TurnID,
			Seq:        1,
			Type:       "turn.started",
			Source:     p.configuredName,
			Visibility: "private",
			Data:       map[string]any{"sessionId": "session-1"},
			CreatedAt:  time.Now(),
			Display: &gestalt.AgentTurnDisplay{
				Kind:  "status",
				Phase: "started",
				Text:  "turn started",
				Input: map[string]any{"turnId": req.TurnID},
			},
		}, {
			ID:         req.TurnID + "-event-2",
			TurnID:     req.TurnID,
			Seq:        2,
			Type:       "interaction.requested",
			Source:     p.configuredName,
			Visibility: "private",
			Data:       map[string]any{"interactionId": "interaction-1"},
			CreatedAt:  time.Now(),
		}},
	}, nil
}

func (p *fullAgentProvider) GetInteraction(_ context.Context, req *gestalt.GetAgentProviderInteractionRequest) (*gestalt.AgentInteraction, error) {
	return &gestalt.AgentInteraction{
		ID:        req.InteractionID,
		Type:      gestalt.AgentInteractionTypeApproval,
		State:     gestalt.AgentInteractionStatePending,
		Title:     "Approve command",
		Prompt:    "Run git status?",
		Request:   map[string]any{"command": "git status"},
		CreatedAt: time.Now(),
		TurnID:    "turn-1",
		SessionID: "session-1",
	}, nil
}

func (p *fullAgentProvider) ListInteractions(_ context.Context, req *gestalt.ListAgentProviderInteractionsRequest) (*gestalt.ListAgentProviderInteractionsResponse, error) {
	return &gestalt.ListAgentProviderInteractionsResponse{
		Interactions: []gestalt.AgentInteraction{{
			ID:        "interaction-1",
			Type:      gestalt.AgentInteractionTypeApproval,
			State:     gestalt.AgentInteractionStatePending,
			Title:     "Approve command",
			Prompt:    "Run git status?",
			CreatedAt: time.Now(),
			TurnID:    req.TurnID,
			SessionID: "session-1",
		}},
	}, nil
}

func (p *fullAgentProvider) ResolveInteraction(_ context.Context, req *gestalt.ResolveAgentProviderInteractionRequest) (*gestalt.AgentInteraction, error) {
	return &gestalt.AgentInteraction{
		ID:         req.InteractionID,
		Type:       gestalt.AgentInteractionTypeApproval,
		State:      gestalt.AgentInteractionStateResolved,
		Title:      "Approve command",
		Prompt:     "Run git status?",
		Resolution: req.Resolution,
		CreatedAt:  time.Now(),
		ResolvedAt: timePtr(time.Now()),
		TurnID:     "turn-1",
		SessionID:  "session-1",
	}, nil
}

func (p *fullAgentProvider) GetCapabilities(context.Context, *gestalt.GetAgentProviderCapabilitiesRequest) (*gestalt.AgentProviderCapabilities, error) {
	return &gestalt.AgentProviderCapabilities{
		StreamingText:             true,
		ToolCalls:                 true,
		Interactions:              true,
		BoundedListHydration:      true,
		SupportedToolSources:      []gestalt.AgentToolSourceMode{gestalt.AgentToolSourceModeCatalog, gestalt.AgentToolSourceModeNone},
		SupportsSessionStart:      true,
		SupportsPreparedWorkspace: true,
	}, nil
}

func TestAgentProviderTypedTransportRoundTrip(t *testing.T) {
	socket := newSocketPath(t, "agent.sock")
	t.Setenv(proto.EnvProviderSocket, socket)
	t.Setenv(proto.EnvProviderName, "agent-test")

	ctx, cancel := context.WithCancel(context.Background())
	provider := &fullAgentProvider{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- gestalt.ServeAgentProvider(ctx, provider)
	}()
	t.Cleanup(func() {
		cancel()
		waitServeResult(t, errCh)
		if !provider.closed.Load() {
			t.Fatal("agent provider Close was not called")
		}
	})

	conn := newUnixConn(t, socket)
	runtimeClient := proto.NewProviderLifecycleClient(conn)
	agentClient := proto.NewAgentClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rpcCancel()

	meta, err := runtimeClient.GetProviderIdentity(rpcCtx, &emptypb.Empty{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetProviderIdentity: %v", err)
	}
	if meta.GetKind() != proto.ProviderKind_PROVIDER_KIND_AGENT {
		t.Fatalf("kind = %v, want AGENT", meta.GetKind())
	}
	if _, err := runtimeClient.ConfigureProvider(rpcCtx, &proto.ConfigureProviderRequest{
		Name:            "agent-test",
		ProtocolVersion: proto.CurrentProtocolVersion,
	}); err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}

	session, err := agentClient.CreateSession(rpcCtx, &proto.CreateAgentProviderSessionRequest{
		IdempotencyKey:     "session-req-1",
		Model:              "gpt-5.1",
		ClientRef:          "client-session-1",
		Metadata:           mustStruct(t, map[string]any{"source": "go-test"}),
		CreatedBySubjectId: "user:user-1",
		Subject: &proto.SubjectContext{
			Id:                  "borrower:borrower-1",
						Email:               "borrower@example.com",
		},
		Context: &proto.RequestContext{
			Subject: &proto.SubjectContext{Id: "user:session"},
			Caller:  &proto.ProviderContext{Kind: "app", Name: "sdk-test"},
		},
		SessionStart: &proto.AgentSessionStartConfig{
			Hooks: []*proto.AgentSessionStartHook{{
				Id:      "hook-1",
				Type:    "command",
				Command: []string{"echo", "hello"},
				Cwd:     "/tmp",
				Timeout: "5s",
				Env:     map[string]string{"A": "B"},
				Output:  &proto.AgentSessionStartHookOutput{AdditionalContext: true},
			}},
		},
		PreparedWorkspace: &proto.PreparedAgentWorkspace{
			Root: "/workspace",
			Cwd:  "/workspace/project",
		},
		Tools: &proto.AgentToolConfig{Source: &proto.AgentToolConfig_Catalog{
			Catalog: &proto.AgentCatalogToolConfig{
				Refs: []*proto.AgentToolRef{{
					App:       "slack",
					Operation: "chat.postMessage",
				}},
				Tools: []*proto.ListedAgentTool{{
					Id:          "tool-slack",
					McpName:     "slack__chat_post_message",
					Title:       "Send Slack message",
					Description: "Post a Slack message",
					InputSchema: `{"type":"object"}`,
					Ref: &proto.AgentToolRef{
						App:       "slack",
						Operation: "chat.postMessage",
					},
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.GetId() != "session-1" || session.GetProviderName() != "agent-test" {
		t.Fatalf("CreateSession = %+v, want session id and configured provider name", session)
	}
	if session.GetMetadata().GetFields()["source"].GetStringValue() != "go-test" {
		t.Fatalf("CreateSession metadata = %+v, want native metadata round trip", session.GetMetadata())
	}
	if session.GetCreatedBySubjectId() != "user:user-1" {
		t.Fatalf("CreateSession created_by_subject_id = %q, want user:user-1", session.GetCreatedBySubjectId())
	}
	if got := provider.receivedSessionRequest.Context.GetSubject().GetId(); got != "user:session" {
		t.Fatalf("native CreateSession context subject = %q, want user:session", got)
	}
	if got := provider.receivedSessionRequest.Context.GetCaller().GetName(); got != "sdk-test" {
		t.Fatalf("native CreateSession context caller = %q, want sdk-test", got)
	}
	sessionCatalog, ok := provider.receivedSessionRequest.Tools.(*gestalt.AgentCatalogToolConfig)
	if !ok {
		t.Fatalf("native CreateSession tools = %#v, want catalog", provider.receivedSessionRequest.Tools)
	}
	if got := sessionCatalog.Refs[0].Operation; got != "chat.postMessage" {
		t.Fatalf("native CreateSession tool ref operation = %q, want chat.postMessage", got)
	}
	if got := sessionCatalog.Tools[0].MCPName; got != "slack__chat_post_message" {
		t.Fatalf("native CreateSession listed tool mcp name = %q, want slack__chat_post_message", got)
	}

	fetchedSession, err := agentClient.GetSession(rpcCtx, &proto.GetAgentProviderSessionRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if fetchedSession.GetState() != proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED {
		t.Fatalf("GetSession state = %v, want archived", fetchedSession.GetState())
	}

	listedSessions, err := agentClient.ListSessions(rpcCtx, &proto.ListAgentProviderSessionsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listedSessions.GetSessions()) != 1 {
		t.Fatalf("ListSessions returned %d sessions, want 1", len(listedSessions.GetSessions()))
	}

	updatedSession, err := agentClient.UpdateSession(rpcCtx, &proto.UpdateAgentProviderSessionRequest{
		SessionId: "session-1",
		ClientRef: "client-session-2",
		State:     proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED,
		Metadata:  mustStruct(t, map[string]any{"updated": true}),
	})
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updatedSession.GetClientRef() != "client-session-2" || updatedSession.GetState() != proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED {
		t.Fatalf("UpdateSession = %+v, want updated client ref and state", updatedSession)
	}

	turn, err := agentClient.CreateTurn(rpcCtx, &proto.CreateAgentProviderTurnRequest{
		TimeoutSeconds: 1,
		TurnId:         "turn-1",
		SessionId:      "session-1",
		IdempotencyKey: "turn-req-1",
		Model:          "gpt-5.1",
		Messages: []*proto.AgentMessage{{
			Role: "user",
			Text: "Plan it",
			Parts: []*proto.AgentMessagePart{{
				Type: proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TEXT,
				Text: "Plan it",
			}, {
				Type: proto.AgentMessagePartType_AGENT_MESSAGE_PART_TYPE_TOOL_CALL,
				ToolCall: &proto.AgentMessagePartToolCall{
					Id:        "tool-call-1",
					ToolId:    "tool-1",
					Arguments: mustStruct(t, map[string]any{"query": "status"}),
				},
			}},
			Metadata: mustStruct(t, map[string]any{"priority": "high"}),
		}},
		Output: &proto.AgentOutput{
			Kind: &proto.AgentOutput_Structured{
				Structured: &proto.AgentStructuredOutput{Schema: mustStruct(t, map[string]any{"type": "object"})},
			},
		},
		Metadata:           mustStruct(t, map[string]any{"requireInteraction": true}),
		CreatedBySubjectId: session.GetCreatedBySubjectId(),
		ExecutionRef:       "exec-turn-1",
		Subject:            &proto.SubjectContext{Id: "borrower:borrower-1"},
		ModelOptions: mustStruct(t, map[string]any{
			"temperature": 0.2,
		}),
		Context: &proto.RequestContext{
			Subject: &proto.SubjectContext{Id: "user:agent-provider"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if turn.GetStatus() != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT {
		t.Fatalf("CreateTurn status = %v, want waiting for input", turn.GetStatus())
	}
	if len(turn.GetMessages()) != 1 || len(turn.GetMessages()[0].GetParts()) != 2 {
		t.Fatalf("CreateTurn messages = %+v, want message parts round trip", turn.GetMessages())
	}
	if turn.GetStructured().GetValue().GetFields()["type"].GetStringValue() != "object" {
		t.Fatalf("CreateTurn structured output = %+v, want native JSON round trip", turn.GetStructured())
	}
	if provider.receivedTurnRequest.Output == nil || provider.receivedTurnRequest.Output.Structured == nil {
		t.Fatalf("CreateTurn output = %#v, want structured output", provider.receivedTurnRequest.Output)
	}
	if provider.receivedTurnRequest.Output.Structured.Schema["type"] != "object" {
		t.Fatalf("CreateTurn output schema = %#v, want object", provider.receivedTurnRequest.Output.Structured.Schema)
	}
	if provider.receivedTurnRequest.Context.GetSubject().GetId() != "user:agent-provider" {
		t.Fatalf("CreateTurn context = %#v, want request context", provider.receivedTurnRequest.Context)
	}
	fetchedTurn, err := agentClient.GetTurn(rpcCtx, &proto.GetAgentProviderTurnRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if fetchedTurn.GetStatus() != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_RUNNING {
		t.Fatalf("GetTurn status = %v, want running", fetchedTurn.GetStatus())
	}

	listedTurns, err := agentClient.ListTurns(rpcCtx, &proto.ListAgentProviderTurnsRequest{SessionId: "session-1"})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(listedTurns.GetTurns()) != 1 || listedTurns.GetTurns()[0].GetStatus() != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED {
		t.Fatalf("ListTurns = %+v, want succeeded turn", listedTurns.GetTurns())
	}

	canceledTurn, err := agentClient.CancelTurn(rpcCtx, &proto.CancelAgentProviderTurnRequest{
		TurnId: "turn-1",
		Reason: "user canceled",
	})
	if err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}
	if canceledTurn.GetStatus() != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED {
		t.Fatalf("CancelTurn status = %v, want canceled", canceledTurn.GetStatus())
	}

	events, err := agentClient.ListTurnEvents(rpcCtx, &proto.ListAgentProviderTurnEventsRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("ListTurnEvents: %v", err)
	}
	if len(events.GetEvents()) != 2 || events.GetEvents()[0].GetDisplay().GetInput().GetStructValue().GetFields()["turnId"].GetStringValue() != "turn-1" {
		t.Fatalf("ListTurnEvents = %+v, want display JSON round trip", events.GetEvents())
	}

	interaction, err := agentClient.GetInteraction(rpcCtx, &proto.GetAgentProviderInteractionRequest{InteractionId: "interaction-1"})
	if err != nil {
		t.Fatalf("GetInteraction: %v", err)
	}
	if interaction.GetState() != proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING {
		t.Fatalf("GetInteraction state = %v, want pending", interaction.GetState())
	}

	interactions, err := agentClient.ListInteractions(rpcCtx, &proto.ListAgentProviderInteractionsRequest{TurnId: "turn-1"})
	if err != nil {
		t.Fatalf("ListInteractions: %v", err)
	}
	if len(interactions.GetInteractions()) != 1 {
		t.Fatalf("ListInteractions returned %d interactions, want 1", len(interactions.GetInteractions()))
	}

	resolved, err := agentClient.ResolveInteraction(rpcCtx, &proto.ResolveAgentProviderInteractionRequest{
		InteractionId: "interaction-1",
		Resolution:    mustStruct(t, map[string]any{"approved": true}),
	})
	if err != nil {
		t.Fatalf("ResolveInteraction: %v", err)
	}
	if resolved.GetState() != proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED {
		t.Fatalf("ResolveInteraction state = %v, want resolved", resolved.GetState())
	}

	capabilities, err := agentClient.GetCapabilities(rpcCtx, &proto.GetAgentProviderCapabilitiesRequest{})
	if err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	if !capabilities.GetStreamingText() || !capabilities.GetToolCalls() || len(capabilities.GetSupportedToolSources()) != 2 {
		t.Fatalf("GetCapabilities = %+v, want streaming text and tool calls", capabilities)
	}
	if capabilities.GetSupportedToolSources()[1] != proto.AgentToolSourceMode_AGENT_TOOL_SOURCE_MODE_NONE {
		t.Fatalf("GetCapabilities supported tool sources = %+v, want none source round trip", capabilities.GetSupportedToolSources())
	}
}

func mustStruct(t *testing.T, value map[string]any) *structpb.Struct {
	t.Helper()
	out, err := structpb.NewStruct(value)
	if err != nil {
		t.Fatalf("structpb.NewStruct(%v): %v", value, err)
	}
	return out
}

func timePtr(value time.Time) *time.Time {
	return &value
}
