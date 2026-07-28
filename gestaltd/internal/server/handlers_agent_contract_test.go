package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type contractHTTPTestService struct {
	createAgent func(context.Context, *principal.Principal, *proto.CreateAgentRequest) (*proto.AgentResource, error)
	listEvents  func(context.Context, *principal.Principal, *proto.ListAgentRunEventsRequest) (*proto.ListAgentRunEventsResponse, error)
}

func (s contractHTTPTestService) CreateAgent(ctx context.Context, p *principal.Principal, req *proto.CreateAgentRequest) (*proto.AgentResource, error) {
	return s.createAgent(ctx, p, req)
}

func (contractHTTPTestService) GetAgent(context.Context, *principal.Principal, *proto.GetAgentRequest) (*proto.AgentResource, error) {
	panic("not used")
}

func (contractHTTPTestService) ListAgents(context.Context, *principal.Principal, *proto.ListAgentsRequest) (*proto.ListAgentsResponse, error) {
	panic("not used")
}

func (contractHTTPTestService) ArchiveAgent(context.Context, *principal.Principal, *proto.ArchiveAgentRequest) (*proto.AgentResource, error) {
	panic("not used")
}

func (contractHTTPTestService) CreateConfigRevision(context.Context, *principal.Principal, *proto.CreateAgentConfigRevisionRequest) (*proto.AgentConfigRevision, error) {
	panic("not used")
}

func (contractHTTPTestService) CreateRun(context.Context, *principal.Principal, *proto.CreateAgentRunRequest) (*proto.AgentRunResource, error) {
	panic("not used")
}

func (contractHTTPTestService) GetRun(context.Context, *principal.Principal, *proto.GetAgentRunRequest) (*proto.AgentRunResource, error) {
	panic("not used")
}

func (contractHTTPTestService) ListRuns(context.Context, *principal.Principal, *proto.ListAgentRunsRequest) (*proto.ListAgentRunsResponse, error) {
	panic("not used")
}

func (contractHTTPTestService) CancelRun(context.Context, *principal.Principal, *proto.CancelAgentRunRequest) (*proto.AgentRunResource, error) {
	panic("not used")
}

func (s contractHTTPTestService) ListRunEvents(ctx context.Context, p *principal.Principal, req *proto.ListAgentRunEventsRequest) (*proto.ListAgentRunEventsResponse, error) {
	return s.listEvents(ctx, p, req)
}

func (contractHTTPTestService) GetRunInteraction(context.Context, *principal.Principal, *proto.GetAgentRunInteractionRequest) (*proto.AgentRunInteraction, error) {
	panic("not used")
}

func (contractHTTPTestService) ListRunInteractions(context.Context, *principal.Principal, *proto.ListAgentRunInteractionsRequest) (*proto.ListAgentRunInteractionsResponse, error) {
	panic("not used")
}

func (contractHTTPTestService) ResolveRunInteraction(context.Context, *principal.Principal, *proto.ResolveAgentRunInteractionRequest) (*proto.AgentRunInteraction, error) {
	panic("not used")
}

func TestCreateContractAgentDerivesIdentityAndIdempotency(t *testing.T) {
	t.Parallel()

	var captured *proto.CreateAgentRequest
	server := &Server{agentContract: contractHTTPTestService{
		createAgent: func(_ context.Context, p *principal.Principal, req *proto.CreateAgentRequest) (*proto.AgentResource, error) {
			if p == nil || p.SubjectID != "user:owner" {
				t.Fatalf("principal = %#v", p)
			}
			captured = req
			return &proto.AgentResource{
				Id:                 "agent_1",
				ProviderName:       "managed",
				State:              proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
				ConfigRevision:     "revision_1",
				CreatedBySubjectId: p.SubjectID,
			}, nil
		},
	}}
	request := contractHTTPTestRequest(
		http.MethodPost,
		"/api/v1/agents",
		`{"config":{"providerName":"managed","model":"gpt-5.5"},"idempotencyKey":"create-1"}`,
		nil,
	)
	request.Header.Set("Idempotency-Key", "create-1")
	response := httptest.NewRecorder()

	server.createContractAgent(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if captured == nil || captured.GetContext() != nil ||
		captured.GetIdempotencyKey() != "create-1" ||
		captured.GetConfig().GetModel() != "gpt-5.5" {
		t.Fatalf("captured request = %#v", captured)
	}
	if !strings.Contains(response.Body.String(), `"id":"agent_1"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestCreateContractAgentRejectsCallerProvidedContext(t *testing.T) {
	t.Parallel()

	called := false
	server := &Server{agentContract: contractHTTPTestService{
		createAgent: func(context.Context, *principal.Principal, *proto.CreateAgentRequest) (*proto.AgentResource, error) {
			called = true
			return nil, nil
		},
	}}
	request := contractHTTPTestRequest(
		http.MethodPost,
		"/api/v1/agents",
		`{"config":{},"context":{"subject":{"id":"user:attacker"}}}`,
		nil,
	)
	response := httptest.NewRecorder()

	server.createContractAgent(response, request)

	if response.Code != http.StatusBadRequest || called {
		t.Fatalf("status = %d, called = %v, body = %s", response.Code, called, response.Body.String())
	}
}

func TestContractAgentRunEventsUsesDurableCursor(t *testing.T) {
	t.Parallel()

	var captured *proto.ListAgentRunEventsRequest
	server := &Server{agentContract: contractHTTPTestService{
		listEvents: func(_ context.Context, _ *principal.Principal, req *proto.ListAgentRunEventsRequest) (*proto.ListAgentRunEventsResponse, error) {
			captured = req
			return &proto.ListAgentRunEventsResponse{Events: []*proto.AgentRunEvent{{
				Id:       "event_2",
				Cursor:   "cursor_2",
				Sequence: 2,
				AgentId:  req.GetAgentId(),
				RunId:    req.GetRunId(),
				Type:     proto.AgentRunEventType_AGENT_RUN_EVENT_TYPE_TEXT_DELTA,
			}}}, nil
		},
	}}
	request := contractHTTPTestRequest(
		http.MethodGet,
		"/api/v1/agents/agent_1/runs/run_1/events?after=cursor_1",
		"",
		map[string]string{"agentID": "agent_1", "runID": "run_1"},
	)
	response := httptest.NewRecorder()

	server.contractAgentRunEvents(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if captured == nil || captured.GetAfterCursor() != "cursor_1" ||
		captured.GetAgentId() != "agent_1" || captured.GetRunId() != "run_1" {
		t.Fatalf("captured request = %#v", captured)
	}
	if !strings.Contains(response.Body.String(), `"cursor":"cursor_2"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func contractHTTPTestRequest(
	method string,
	target string,
	body string,
	params map[string]string,
) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	ctx := principal.WithPrincipal(request.Context(), &principal.Principal{
		SubjectID: "user:owner",
	})
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	ctx = context.WithValue(ctx, chi.RouteCtxKey, routeContext)
	return request.WithContext(ctx)
}

var _ agentmanager.ContractService = contractHTTPTestService{}
