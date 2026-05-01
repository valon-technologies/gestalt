package agents

import (
	"context"
	"errors"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recordingManagerService struct {
	listSessions func(context.Context, *principal.Principal, coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error)
	listTurns    func(context.Context, *principal.Principal, coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error)
}

func (s *recordingManagerService) CreateSession(context.Context, *principal.Principal, coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("unexpected CreateSession call")
}

func (s *recordingManagerService) GetSession(context.Context, *principal.Principal, string) (*coreagent.Session, error) {
	return nil, errors.New("unexpected GetSession call")
}

func (s *recordingManagerService) ListSessions(ctx context.Context, p *principal.Principal, req coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error) {
	if s.listSessions != nil {
		return s.listSessions(ctx, p, req)
	}
	return nil, errors.New("unexpected ListSessions call")
}

func (s *recordingManagerService) UpdateSession(context.Context, *principal.Principal, coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("unexpected UpdateSession call")
}

func (s *recordingManagerService) CreateTurn(context.Context, *principal.Principal, coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	return nil, errors.New("unexpected CreateTurn call")
}

func (s *recordingManagerService) GetTurn(context.Context, *principal.Principal, string) (*coreagent.Turn, error) {
	return nil, errors.New("unexpected GetTurn call")
}

func (s *recordingManagerService) ListTurns(ctx context.Context, p *principal.Principal, req coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error) {
	if s.listTurns != nil {
		return s.listTurns(ctx, p, req)
	}
	return nil, errors.New("unexpected ListTurns call")
}

func (s *recordingManagerService) CancelTurn(context.Context, *principal.Principal, string, string) (*coreagent.Turn, error) {
	return nil, errors.New("unexpected CancelTurn call")
}

func (s *recordingManagerService) ListTurnEvents(context.Context, *principal.Principal, string, int64, int) ([]*coreagent.TurnEvent, error) {
	return nil, errors.New("unexpected ListTurnEvents call")
}

func (s *recordingManagerService) ListInteractions(context.Context, *principal.Principal, string) ([]*coreagent.Interaction, error) {
	return nil, errors.New("unexpected ListInteractions call")
}

func (s *recordingManagerService) ResolveInteraction(context.Context, *principal.Principal, string, string, map[string]any) (*coreagent.Interaction, error) {
	return nil, errors.New("unexpected ResolveInteraction call")
}

func TestManagerServerForwardsBoundedListRequests(t *testing.T) {
	t.Parallel()

	tokens, err := NewInvocationTokenManager([]byte("agent-manager-server-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: "user-1",
		Kind:      principal.KindUser,
	})
	token, err := tokens.MintRootToken(ctx, "caller-plugin", nil)
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}

	service := &recordingManagerService{
		listSessions: func(_ context.Context, p *principal.Principal, req coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error) {
			if p == nil || p.SubjectID != "user-1" {
				t.Fatalf("principal = %#v, want subject user-1", p)
			}
			if req.ProviderName != "managed" || req.State != coreagent.SessionStateActive || req.Limit != 7 || !req.SummaryOnly {
				t.Fatalf("list sessions req = %#v", req)
			}
			return []*coreagent.Session{{
				ID:       "session-1",
				State:    coreagent.SessionStateActive,
				Metadata: map[string]any{"heavy": "value"},
			}}, nil
		},
		listTurns: func(_ context.Context, p *principal.Principal, req coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error) {
			if p == nil || p.SubjectID != "user-1" {
				t.Fatalf("principal = %#v, want subject user-1", p)
			}
			if req.SessionID != "session-1" || req.Status != coreagent.ExecutionStatusSucceeded || req.Limit != 3 || !req.SummaryOnly {
				t.Fatalf("list turns req = %#v", req)
			}
			return []*coreagent.Turn{{
				ID:               "turn-1",
				SessionID:        "session-1",
				Status:           coreagent.ExecutionStatusSucceeded,
				Messages:         []coreagent.Message{{Role: "user", Text: "heavy"}},
				OutputText:       "heavy output",
				StructuredOutput: map[string]any{"heavy": "value"},
			}}, nil
		},
	}
	server := NewManagerServer("caller-plugin", service, tokens)

	if _, err := server.ListSessions(context.Background(), &proto.AgentManagerListSessionsRequest{
		InvocationToken: token,
		Limit:           -1,
		SummaryOnly:     true,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListSessions negative limit code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}

	sessions, err := server.ListSessions(context.Background(), &proto.AgentManagerListSessionsRequest{
		ProviderName:    " managed ",
		InvocationToken: token,
		State:           proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
		Limit:           7,
		SummaryOnly:     true,
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got := sessions.GetSessions(); len(got) != 1 || got[0].GetId() != "session-1" {
		t.Fatalf("sessions = %#v", got)
	} else if got[0].GetMetadata().GetFields()["heavy"].GetStringValue() != "value" {
		t.Fatalf("summary session metadata = %#v, want manager result preserved", got[0].GetMetadata())
	}

	if _, err := server.ListTurns(context.Background(), &proto.AgentManagerListTurnsRequest{
		SessionId:       "session-1",
		InvocationToken: token,
		Limit:           -1,
		SummaryOnly:     true,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListTurns negative limit code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}

	turns, err := server.ListTurns(context.Background(), &proto.AgentManagerListTurnsRequest{
		SessionId:       "session-1",
		InvocationToken: token,
		Status:          proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_SUCCEEDED,
		Limit:           3,
		SummaryOnly:     true,
	})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if got := turns.GetTurns(); len(got) != 1 || got[0].GetId() != "turn-1" {
		t.Fatalf("turns = %#v", got)
	} else {
		turn := got[0]
		if len(turn.GetMessages()) != 1 || turn.GetMessages()[0].GetText() != "heavy" || turn.GetOutputText() != "heavy output" || turn.GetStructuredOutput().GetFields()["heavy"].GetStringValue() != "value" {
			t.Fatalf("summary turn = %#v, want manager result preserved", turn)
		}
	}
}
