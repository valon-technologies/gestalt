package providerhost

import (
	"context"
	"errors"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/agentgrant"
	"github.com/valon-technologies/gestalt/server/internal/agentmanager"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recordingAgentManagerService struct {
	listSessions func(context.Context, *principal.Principal, coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error)
	listTurns    func(context.Context, *principal.Principal, coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error)
}

func (s *recordingAgentManagerService) CreateSession(context.Context, *principal.Principal, coreagent.ManagerCreateSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("unexpected CreateSession call")
}

func (s *recordingAgentManagerService) GetSession(context.Context, *principal.Principal, string) (*coreagent.Session, error) {
	return nil, errors.New("unexpected GetSession call")
}

func (s *recordingAgentManagerService) ListSessions(ctx context.Context, p *principal.Principal, req coreagent.ManagerListSessionsRequest) ([]*coreagent.Session, error) {
	if s.listSessions != nil {
		return s.listSessions(ctx, p, req)
	}
	return nil, errors.New("unexpected ListSessions call")
}

func (s *recordingAgentManagerService) UpdateSession(context.Context, *principal.Principal, coreagent.ManagerUpdateSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("unexpected UpdateSession call")
}

func (s *recordingAgentManagerService) CreateTurn(context.Context, *principal.Principal, coreagent.ManagerCreateTurnRequest) (*coreagent.Turn, error) {
	return nil, errors.New("unexpected CreateTurn call")
}

func (s *recordingAgentManagerService) GetTurn(context.Context, *principal.Principal, string) (*coreagent.Turn, error) {
	return nil, errors.New("unexpected GetTurn call")
}

func (s *recordingAgentManagerService) ListTurns(ctx context.Context, p *principal.Principal, req coreagent.ManagerListTurnsRequest) ([]*coreagent.Turn, error) {
	if s.listTurns != nil {
		return s.listTurns(ctx, p, req)
	}
	return nil, errors.New("unexpected ListTurns call")
}

func (s *recordingAgentManagerService) CancelTurn(context.Context, *principal.Principal, string, string) (*coreagent.Turn, error) {
	return nil, errors.New("unexpected CancelTurn call")
}

func (s *recordingAgentManagerService) ListTurnEvents(context.Context, *principal.Principal, string, int64, int) ([]*coreagent.TurnEvent, error) {
	return nil, errors.New("unexpected ListTurnEvents call")
}

func (s *recordingAgentManagerService) ListInteractions(context.Context, *principal.Principal, string) ([]*coreagent.Interaction, error) {
	return nil, errors.New("unexpected ListInteractions call")
}

func (s *recordingAgentManagerService) ResolveInteraction(context.Context, *principal.Principal, string, string, map[string]any) (*coreagent.Interaction, error) {
	return nil, errors.New("unexpected ResolveInteraction call")
}

type lateAcceptedTurnAgentControl struct {
	provider *lateAcceptedTurnAgentProvider
}

func (c lateAcceptedTurnAgentControl) ResolveProviderSelection(name string) (string, coreagent.Provider, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(name) == "managed" {
		return "managed", c.provider, nil
	}
	return "", nil, agentmanager.NewAgentProviderNotAvailableError(name)
}

func (c lateAcceptedTurnAgentControl) ResolveProvider(name string) (coreagent.Provider, error) {
	_, provider, err := c.ResolveProviderSelection(name)
	return provider, err
}

func (c lateAcceptedTurnAgentControl) ProviderNames() []string {
	return []string{"managed"}
}

type lateAcceptedTurnAgentProvider struct {
	coreagent.UnimplementedProvider
	session      *coreagent.Session
	acceptedTurn *coreagent.Turn
	getTurnCalls int
}

func (p *lateAcceptedTurnAgentProvider) CreateSession(_ context.Context, req coreagent.CreateSessionRequest) (*coreagent.Session, error) {
	p.session = &coreagent.Session{
		ID:           req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		State:        coreagent.SessionStateActive,
		CreatedBy:    req.CreatedBy,
	}
	return p.session, nil
}

func (p *lateAcceptedTurnAgentProvider) GetSession(_ context.Context, req coreagent.GetSessionRequest) (*coreagent.Session, error) {
	if p.session == nil || strings.TrimSpace(req.SessionID) != p.session.ID {
		return nil, core.ErrNotFound
	}
	return p.session, nil
}

func (p *lateAcceptedTurnAgentProvider) CreateTurn(_ context.Context, req coreagent.CreateTurnRequest) (*coreagent.Turn, error) {
	p.acceptedTurn = &coreagent.Turn{
		ID:           req.TurnID,
		SessionID:    req.SessionID,
		ProviderName: "managed",
		Model:        req.Model,
		Status:       coreagent.ExecutionStatusRunning,
		Messages:     append([]coreagent.Message(nil), req.Messages...),
		CreatedBy:    req.CreatedBy,
		ExecutionRef: req.ExecutionRef,
	}
	return nil, context.DeadlineExceeded
}

func (p *lateAcceptedTurnAgentProvider) GetTurn(_ context.Context, req coreagent.GetTurnRequest) (*coreagent.Turn, error) {
	p.getTurnCalls++
	if p.getTurnCalls == 1 || p.acceptedTurn == nil || strings.TrimSpace(req.TurnID) != p.acceptedTurn.ID {
		return nil, core.ErrNotFound
	}
	return p.acceptedTurn, nil
}

func (p *lateAcceptedTurnAgentProvider) GetCapabilities(context.Context, coreagent.GetCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return &coreagent.ProviderCapabilities{NativeToolSearch: true}, nil
}

func TestAgentManagerServerCreateTurnRecoversLateAcceptedProviderTurn(t *testing.T) {
	provider := &lateAcceptedTurnAgentProvider{}
	grants, err := agentgrant.NewManager([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("agentgrant.NewManager: %v", err)
	}
	manager := agentmanager.New(agentmanager.Config{
		Agent:      lateAcceptedTurnAgentControl{provider: provider},
		ToolGrants: grants,
	})
	tokens, err := NewInvocationTokenManager([]byte("agent-manager-server-test-secret"))
	if err != nil {
		t.Fatalf("NewInvocationTokenManager: %v", err)
	}
	ctx := principal.WithPrincipal(context.Background(), &principal.Principal{
		SubjectID: principal.UserSubjectID("user-1"),
		Kind:      principal.KindUser,
	})
	token, err := tokens.MintRootToken(ctx, "caller-plugin", nil)
	if err != nil {
		t.Fatalf("MintRootToken: %v", err)
	}
	server := NewAgentManagerServer("caller-plugin", manager, tokens)
	conn := newBufconnConn(t, func(srv *grpc.Server) {
		proto.RegisterAgentManagerHostServer(srv, server)
	})
	client := proto.NewAgentManagerHostClient(conn)

	session, err := client.CreateSession(context.Background(), &proto.AgentManagerCreateSessionRequest{
		InvocationToken: token,
		ProviderName:    "managed",
		Model:           "test-model",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	turn, err := client.CreateTurn(context.Background(), &proto.AgentManagerCreateTurnRequest{
		InvocationToken: token,
		SessionId:       session.GetId(),
		Model:           "test-model",
		IdempotencyKey:  "workflow:provider:run-1:turn:batch-1",
		Messages: []*proto.AgentMessage{{
			Role: "user",
			Text: "hello",
		}},
	})
	if err != nil {
		t.Fatalf("CreateTurn: %v", err)
	}
	if provider.acceptedTurn == nil || turn.GetId() != provider.acceptedTurn.ID {
		t.Fatalf("CreateTurn returned %q, want accepted provider turn %#v", turn.GetId(), provider.acceptedTurn)
	}
	if provider.getTurnCalls < 2 {
		t.Fatalf("GetTurn calls = %d, want retry after initial miss", provider.getTurnCalls)
	}
}

func TestAgentManagerServerForwardsBoundedListRequests(t *testing.T) {
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

	service := &recordingAgentManagerService{
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
	server := NewAgentManagerServer("caller-plugin", service, tokens)

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
