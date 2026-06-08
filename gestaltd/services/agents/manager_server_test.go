package agents

import (
	"context"
	"errors"
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type recordingManagerService struct {
	createSession func(context.Context, *principal.Principal, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error)
	createTurn    func(context.Context, *principal.Principal, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error)
	getTurn       func(context.Context, *principal.Principal, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error)
	cancelTurn    func(context.Context, *principal.Principal, *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error)
	listSessions  func(context.Context, *principal.Principal, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error)
	listTurns     func(context.Context, *principal.Principal, *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error)
}

func (s *recordingManagerService) CreateSession(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	if s.createSession != nil {
		return s.createSession(ctx, p, req)
	}
	return nil, errors.New("unexpected CreateSession call")
}

func (s *recordingManagerService) GetSession(context.Context, *principal.Principal, *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("unexpected GetSession call")
}

func (s *recordingManagerService) ListSessions(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	if s.listSessions != nil {
		return s.listSessions(ctx, p, req)
	}
	return nil, errors.New("unexpected ListSessions call")
}

func (s *recordingManagerService) UpdateSession(context.Context, *principal.Principal, *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("unexpected UpdateSession call")
}

func (s *recordingManagerService) CreateTurn(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if s.createTurn != nil {
		return s.createTurn(ctx, p, req)
	}
	return nil, errors.New("unexpected CreateTurn call")
}

func (s *recordingManagerService) GetTurn(ctx context.Context, p *principal.Principal, req *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if s.getTurn != nil {
		return s.getTurn(ctx, p, req)
	}
	return nil, errors.New("unexpected GetTurn call")
}

func (s *recordingManagerService) ListTurns(ctx context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	if s.listTurns != nil {
		return s.listTurns(ctx, p, req)
	}
	return nil, errors.New("unexpected ListTurns call")
}

func (s *recordingManagerService) ExecuteTool(context.Context, *principal.Principal, coreagent.ExecuteToolRequest) (*coreagent.ExecuteToolResponse, error) {
	return nil, errors.New("unexpected ExecuteTool call")
}

func (s *recordingManagerService) CancelTurn(ctx context.Context, p *principal.Principal, req *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	if s.cancelTurn != nil {
		return s.cancelTurn(ctx, p, req)
	}
	return nil, errors.New("unexpected CancelTurn call")
}

func (s *recordingManagerService) ListTurnEvents(context.Context, *principal.Principal, *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	return nil, errors.New("unexpected ListTurnEvents call")
}

func (s *recordingManagerService) ListInteractions(context.Context, *principal.Principal, *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	return nil, errors.New("unexpected ListInteractions call")
}

func (s *recordingManagerService) ResolveInteraction(context.Context, *principal.Principal, *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	return nil, errors.New("unexpected ResolveInteraction call")
}

func TestManagerServerMapsSessionStartUnsupportedToFailedPrecondition(t *testing.T) {
	t.Parallel()

	server := NewProviderServer("caller-plugin", &recordingManagerService{
		createSession: func(context.Context, *principal.Principal, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
			return nil, agentmanager.ErrAgentSessionStartUnsupported
		},
	})

	_, err := server.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		Context:      agentManagerRequestContext("caller-plugin", "user-1", nil),
		ProviderName: "managed",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CreateSession code = %v, want %v", status.Code(err), codes.FailedPrecondition)
	}
}

func TestManagerServerMapsInvalidSessionMetadataToInvalidArgument(t *testing.T) {
	t.Parallel()

	server := NewProviderServer("caller-plugin", &recordingManagerService{
		createSession: func(context.Context, *principal.Principal, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
			return nil, agentmanager.ErrAgentSessionMetadataInvalid
		},
	})

	_, err := server.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		Context:      agentManagerRequestContext("caller-plugin", "user-1", nil),
		ProviderName: "managed",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateSession code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}

func TestManagerServerCreateTurnForwardsStructuredOutputInputs(t *testing.T) {
	t.Parallel()

	workflow := map[string]any{
		"providerName": "indexeddb",
		"runId":        "run-123",
	}
	server := NewProviderServer("caller-plugin", &recordingManagerService{
		createTurn: func(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
			if p == nil || p.SubjectID != "user-1" {
				t.Fatalf("principal = %#v, want subject user-1", p)
			}
			if got := invocation.WorkflowContextString(invocation.WorkflowContextFromContext(ctx), "runId"); got != "run-123" {
				t.Fatalf("workflow run id = %q, want run-123", got)
			}
			if req.GetOutput().GetStructured() == nil {
				t.Fatal("output.structured = nil, want structured output request")
			}
			if req.GetOutput().GetStructured().GetSchema().AsMap()["type"] != "object" {
				t.Fatalf("response schema = %#v, want object schema", req.GetOutput().GetStructured().GetSchema())
			}
			return &coreagent.Turn{
				ID:        "turn-1",
				SessionID: req.GetSessionId(),
				Status:    coreagent.ExecutionStatusRunning,
			}, nil
		},
	})

	schema, err := structpb.NewStruct(map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	_, err = server.CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		Context:        agentManagerRequestContext("caller-plugin", "user-1", workflow),
		TimeoutSeconds: 1,
		SessionId:      "session-1",
		Output: &proto.AgentOutput{Kind: &proto.AgentOutput_Structured{
			Structured: &proto.AgentStructuredOutput{Schema: schema},
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
}

func TestManagerServerAuthorizesWorkflowAgentStep(t *testing.T) {
	t.Parallel()

	workflow := map[string]any{
		"providerName":         "temporal",
		"runId":                "run-123",
		"definitionId":         "agent_review",
		"definitionGeneration": 2,
		"workflowKey":          "review:123",
		"currentStepId":        "review",
		"currentStep": map[string]any{
			"id":    "review",
			"index": 0,
		},
		"target": map[string]any{
			"kind": "steps",
			"steps": []any{
				map[string]any{
					"id":            "review",
					"kind":          "agent",
					"agentProvider": "claude",
					"model":         "default",
				},
			},
		},
	}
	service := &recordingManagerService{
		createSession: func(ctx context.Context, p *principal.Principal, req *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
			if got := invocation.WorkflowContextString(invocation.WorkflowContextFromContext(ctx), "currentStepId"); got != "review" {
				t.Fatalf("workflow currentStepId = %q, want review", got)
			}
			return &coreagent.Session{ID: "session-1", ProviderName: req.GetProviderName(), Model: req.GetModel(), State: coreagent.SessionStateActive}, nil
		},
		createTurn: func(context.Context, *principal.Principal, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
			return &coreagent.Turn{ID: "turn-1", SessionID: "session-1", Status: coreagent.ExecutionStatusRunning}, nil
		},
		getTurn: func(context.Context, *principal.Principal, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
			return &coreagent.Turn{ID: "turn-1", SessionID: "session-1", Status: coreagent.ExecutionStatusSucceeded}, nil
		},
		cancelTurn: func(context.Context, *principal.Principal, *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
			return &coreagent.Turn{ID: "turn-1", SessionID: "session-1", Status: coreagent.ExecutionStatusCanceled}, nil
		},
	}
	server := NewProviderServer("temporal", service)
	ctx := agentManagerRequestContextWithCallerKind("workflow", "temporal", "user-1", workflow)

	if _, err := server.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		Context:      ctx,
		ProviderName: "claude",
		Model:        "default",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := server.CreateTurn(context.Background(), &proto.CreateAgentProviderTurnRequest{
		Context:   ctx,
		SessionId: "session-1",
		Model:     "default",
	}); err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if _, err := server.GetTurn(context.Background(), &proto.GetAgentProviderTurnRequest{
		Context: ctx,
		TurnId:  "turn-1",
	}); err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if _, err := server.CancelTurn(context.Background(), &proto.CancelAgentProviderTurnRequest{
		Context: ctx,
		TurnId:  "turn-1",
	}); err != nil {
		t.Fatalf("CancelTurn: %v", err)
	}

	_, err := server.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		Context:      ctx,
		ProviderName: "openai",
		Model:        "default",
	})
	if got := status.Code(err); got != codes.PermissionDenied {
		t.Fatalf("CreateSession mismatched provider status = %s, want %s (err=%v)", got, codes.PermissionDenied, err)
	}
}

func TestManagerServerForwardsBoundedListRequests(t *testing.T) {
	t.Parallel()

	service := &recordingManagerService{
		listSessions: func(_ context.Context, p *principal.Principal, req *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
			if p == nil || p.SubjectID != "user-1" {
				t.Fatalf("principal = %#v, want subject user-1", p)
			}
			if req.GetProviderName() != " managed " || req.GetState() != proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE || req.GetLimit() != 7 || !req.GetSummaryOnly() {
				t.Fatalf("list sessions req = %#v", req)
			}
			return []*coreagent.Session{{
				ID:       "session-1",
				State:    coreagent.SessionStateActive,
				Metadata: map[string]any{"heavy": "value"},
			}}, nil
		},
		listTurns: func(_ context.Context, p *principal.Principal, req *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
			if p == nil || p.SubjectID != "user-1" {
				t.Fatalf("principal = %#v, want subject user-1", p)
			}
			if req.GetSessionId() != "session-1" || req.GetStatus() != proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED || req.GetLimit() != 3 || !req.GetSummaryOnly() {
				t.Fatalf("list turns req = %#v", req)
			}
			return []*coreagent.Turn{{
				ID:        "turn-1",
				SessionID: "session-1",
				Status:    coreagent.ExecutionStatusSucceeded,
				Messages:  []coreagent.Message{{Role: "user", Text: "heavy"}},
				Output: coreagent.TurnOutput{
					Structured: &coreagent.TurnStructuredOutput{
						Text:  "heavy output",
						Value: map[string]any{"heavy": "value"},
					},
				},
			}}, nil
		},
	}
	server := NewProviderServer("caller-plugin", service)

	if _, err := server.ListSessions(context.Background(), &proto.ListAgentProviderSessionsRequest{
		Context:     agentManagerRequestContext("caller-plugin", "user-1", nil),
		Limit:       -1,
		SummaryOnly: true,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListSessions negative limit code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}

	sessions, err := server.ListSessions(context.Background(), &proto.ListAgentProviderSessionsRequest{
		Context:      agentManagerRequestContext("caller-plugin", "user-1", nil),
		ProviderName: " managed ",
		State:        proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
		Limit:        7,
		SummaryOnly:  true,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := sessions.GetSessions(); len(got) != 1 || got[0].GetId() != "session-1" {
		t.Fatalf("sessions = %#v", got)
	} else if got[0].GetMetadata().GetFields()["heavy"].GetStringValue() != "value" {
		t.Fatalf("summary session metadata = %#v, want manager result preserved", got[0].GetMetadata())
	}

	if _, err := server.ListTurns(context.Background(), &proto.ListAgentProviderTurnsRequest{
		Context:     agentManagerRequestContext("caller-plugin", "user-1", nil),
		SessionId:   "session-1",
		Limit:       -1,
		SummaryOnly: true,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListTurns negative limit code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}

	turns, err := server.ListTurns(context.Background(), &proto.ListAgentProviderTurnsRequest{
		Context:     agentManagerRequestContext("caller-plugin", "user-1", nil),
		SessionId:   "session-1",
		Status:      proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED,
		Limit:       3,
		SummaryOnly: true,
	})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if got := turns.GetTurns(); len(got) != 1 || got[0].GetId() != "turn-1" {
		t.Fatalf("turns = %#v", got)
	} else {
		turn := got[0]
		if len(turn.GetMessages()) != 1 || turn.GetMessages()[0].GetText() != "heavy" || turn.GetStructured().GetText() != "heavy output" || turn.GetStructured().GetValue().GetFields()["heavy"].GetStringValue() != "value" {
			t.Fatalf("summary turn = %#v, want manager result preserved", turn)
		}
	}
}

func agentManagerRequestContext(callerApp, subjectID string, workflow map[string]any) *proto.RequestContext {
	return agentManagerRequestContextWithCallerKind("app", callerApp, subjectID, workflow)
}

func agentManagerRequestContextWithCallerKind(callerKind, callerApp, subjectID string, workflow map[string]any) *proto.RequestContext {
	ctx := &proto.RequestContext{
		Caller: &proto.ProviderContext{
			Kind: callerKind,
			Name: callerApp,
		},
		Subject: &proto.SubjectContext{
			Id:                  subjectID,
			CredentialSubjectId: subjectID,
		},
	}
	if workflow != nil {
		value, err := structpb.NewStruct(workflow)
		if err != nil {
			panic(err)
		}
		ctx.Workflow = value
	}
	return ctx
}
