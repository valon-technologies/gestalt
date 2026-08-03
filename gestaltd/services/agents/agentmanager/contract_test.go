package agentmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentroute"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	gproto "google.golang.org/protobuf/proto"
)

type contractTestControl struct {
	name     string
	provider coreagent.Provider
}

func (c contractTestControl) ResolveProvider(_ context.Context, name string) (string, coreagent.Provider, error) {
	if name = strings.TrimSpace(name); name != "" && name != c.name {
		return "", nil, NewAgentProviderNotAvailableError(name)
	}
	return c.name, c.provider, nil
}

func (c contractTestControl) ProviderNames() []string {
	return []string{c.name}
}

type contractTestProvider struct {
	coreagent.UnimplementedProvider

	createSessionCalls int
	sessions           map[string]*proto.AgentResource
	runs               map[string]*proto.AgentRunResource
	events             map[string][]*proto.AgentRunEvent
	interactions       map[string]*proto.AgentRunInteraction
}

func newContractTestProvider() *contractTestProvider {
	return &contractTestProvider{
		sessions:     map[string]*proto.AgentResource{},
		runs:         map[string]*proto.AgentRunResource{},
		events:       map[string][]*proto.AgentRunEvent{},
		interactions: map[string]*proto.AgentRunInteraction{},
	}
}

func (p *contractTestProvider) CreateContractSession(
	_ context.Context,
	req *proto.AgentProviderCreateSessionRequest,
) (*proto.AgentResource, error) {
	p.createSessionCalls++
	if existing := p.sessions[req.GetSessionId()]; existing != nil {
		return gproto.Clone(existing).(*proto.AgentResource), nil
	}
	resource := &proto.AgentResource{
		Id:                 req.GetSessionId(),
		State:              proto.AgentSessionState_AGENT_SESSION_STATE_ACTIVE,
		ConfigRevision:     req.GetInitialConfig().GetId(),
		CreatedBySubjectId: req.GetCreatedBySubjectId(),
	}
	p.sessions[resource.GetId()] = resource
	return gproto.Clone(resource).(*proto.AgentResource), nil
}

func (p *contractTestProvider) GetContractSession(
	_ context.Context,
	req *proto.AgentProviderGetSessionRequest,
) (*proto.AgentResource, error) {
	resource := p.sessions[req.GetSessionId()]
	if resource == nil {
		return nil, core.ErrNotFound
	}
	return gproto.Clone(resource).(*proto.AgentResource), nil
}

func (p *contractTestProvider) ListContractSessions(
	context.Context,
	*proto.AgentProviderListSessionsRequest,
) (*proto.AgentProviderListSessionsResponse, error) {
	response := &proto.AgentProviderListSessionsResponse{}
	for _, resource := range p.sessions {
		response.Sessions = append(response.Sessions, gproto.Clone(resource).(*proto.AgentResource))
	}
	return response, nil
}

func (p *contractTestProvider) ArchiveContractSession(
	_ context.Context,
	req *proto.AgentProviderArchiveSessionRequest,
) (*proto.AgentResource, error) {
	resource := p.sessions[req.GetSessionId()]
	if resource == nil {
		return nil, core.ErrNotFound
	}
	resource.State = proto.AgentSessionState_AGENT_SESSION_STATE_ARCHIVED
	return gproto.Clone(resource).(*proto.AgentResource), nil
}

func (p *contractTestProvider) CreateContractConfigRevision(
	context.Context,
	*proto.AgentProviderCreateConfigRevisionRequest,
) (*proto.AgentResolvedConfigRevision, error) {
	return nil, errors.New("not used")
}

func (p *contractTestProvider) CreateContractRun(
	_ context.Context,
	req *proto.AgentProviderCreateRunRequest,
) (*proto.AgentRunResource, error) {
	key := req.GetSessionId() + "/" + req.GetRunId()
	if existing := p.runs[key]; existing != nil {
		return gproto.Clone(existing).(*proto.AgentRunResource), nil
	}
	run := &proto.AgentRunResource{
		Id:             req.GetRunId(),
		AgentId:        req.GetSessionId(),
		ConfigRevision: req.GetConfigRevision(),
		Status:         proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_PENDING,
	}
	p.runs[key] = run
	return gproto.Clone(run).(*proto.AgentRunResource), nil
}

func (p *contractTestProvider) GetContractRun(
	_ context.Context,
	req *proto.AgentProviderGetRunRequest,
) (*proto.AgentRunResource, error) {
	run := p.runs[req.GetSessionId()+"/"+req.GetRunId()]
	if run == nil {
		return nil, core.ErrNotFound
	}
	return gproto.Clone(run).(*proto.AgentRunResource), nil
}

func (p *contractTestProvider) ListContractRuns(
	_ context.Context,
	req *proto.AgentProviderListRunsRequest,
) (*proto.ListAgentRunsResponse, error) {
	response := &proto.ListAgentRunsResponse{}
	prefix := req.GetSessionId() + "/"
	for key, run := range p.runs {
		if strings.HasPrefix(key, prefix) {
			response.Runs = append(response.Runs, gproto.Clone(run).(*proto.AgentRunResource))
		}
	}
	return response, nil
}

func (p *contractTestProvider) CancelContractRun(
	_ context.Context,
	req *proto.AgentProviderCancelRunRequest,
) (*proto.AgentRunResource, error) {
	run := p.runs[req.GetSessionId()+"/"+req.GetRunId()]
	if run == nil {
		return nil, core.ErrNotFound
	}
	run.Status = proto.AgentExecutionStatus_AGENT_EXECUTION_STATUS_CANCELED
	return gproto.Clone(run).(*proto.AgentRunResource), nil
}

func (p *contractTestProvider) ListContractRunEvents(
	_ context.Context,
	req *proto.AgentProviderListRunEventsRequest,
) (*proto.ListAgentRunEventsResponse, error) {
	response := &proto.ListAgentRunEventsResponse{}
	for _, event := range p.events[req.GetSessionId()+"/"+req.GetRunId()] {
		response.Events = append(response.Events, gproto.Clone(event).(*proto.AgentRunEvent))
	}
	return response, nil
}

func (p *contractTestProvider) GetContractInteraction(
	_ context.Context,
	req *proto.AgentProviderGetInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	interaction := p.interactions[req.GetSessionId()+"/"+req.GetRunId()+"/"+req.GetInteractionId()]
	if interaction == nil {
		return nil, core.ErrNotFound
	}
	return gproto.Clone(interaction).(*proto.AgentRunInteraction), nil
}

func (p *contractTestProvider) ListContractInteractions(
	_ context.Context,
	req *proto.AgentProviderListInteractionsRequest,
) (*proto.ListAgentRunInteractionsResponse, error) {
	response := &proto.ListAgentRunInteractionsResponse{}
	prefix := req.GetSessionId() + "/" + req.GetRunId() + "/"
	for key, interaction := range p.interactions {
		if strings.HasPrefix(key, prefix) &&
			(req.GetState() == proto.AgentInteractionState_AGENT_INTERACTION_STATE_UNSPECIFIED ||
				req.GetState() == interaction.GetState()) {
			response.Interactions = append(
				response.Interactions,
				gproto.Clone(interaction).(*proto.AgentRunInteraction),
			)
		}
	}
	return response, nil
}

func (p *contractTestProvider) ResolveContractInteraction(
	_ context.Context,
	req *proto.AgentProviderResolveInteractionRequest,
) (*proto.AgentRunInteraction, error) {
	key := req.GetSessionId() + "/" + req.GetRunId() + "/" + req.GetInteractionId()
	interaction := p.interactions[key]
	if interaction == nil {
		return nil, core.ErrNotFound
	}
	interaction.State = proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED
	return gproto.Clone(interaction).(*proto.AgentRunInteraction), nil
}

func (p *contractTestProvider) GetContractCapabilities(
	context.Context,
	*proto.GetAgentProviderContractCapabilitiesRequest,
) (*proto.AgentProviderContractCapabilities, error) {
	return &proto.AgentProviderContractCapabilities{
		ProtocolVersion: coreagent.ContractProtocolVersion,
		Interactions:    true,
	}, nil
}

func newContractTestManager(t testing.TB, provider *contractTestProvider) *Manager {
	t.Helper()
	routes, err := agentroute.NewIndexedDBStore(context.Background(), &coretesting.StubIndexedDB{})
	if err != nil {
		t.Fatalf("NewIndexedDBStore: %v", err)
	}
	return newTestManager(t, Config{
		Agent: contractTestControl{
			name:     "managed",
			provider: provider,
		},
		Routes: routes,
	})
}

func TestContractCreateResumeAndOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	provider := newContractTestProvider()
	manager := newContractTestManager(t, provider)
	owner := &principal.Principal{SubjectID: "user:owner"}
	request := &proto.CreateAgentRequest{
		Config: &proto.AgentConfigInput{
			ProviderName: " managed ",
			Model:        " gpt-5.5 ",
			Instructions: "be useful",
			Tools:        &proto.AgentToolSelection{Disabled: true},
		},
		IdempotencyKey: "create-1",
	}
	created, err := manager.CreateAgent(ctx, owner, request)
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if created.GetId() == "" || created.GetProviderName() != "managed" {
		t.Fatalf("CreateAgent = %#v", created)
	}
	replayed, err := manager.CreateAgent(ctx, owner, request)
	if err != nil {
		t.Fatalf("CreateAgent replay: %v", err)
	}
	if replayed.GetId() != created.GetId() || provider.createSessionCalls != 1 {
		t.Fatalf("CreateAgent replay = %#v, provider calls = %d", replayed, provider.createSessionCalls)
	}
	resumed, err := manager.GetAgent(ctx, owner, &proto.GetAgentRequest{AgentId: created.GetId()})
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if resumed.GetConfigRevision() != created.GetConfigRevision() {
		t.Fatalf("GetAgent revision = %q, want %q", resumed.GetConfigRevision(), created.GetConfigRevision())
	}
	_, err = manager.GetAgent(ctx, &principal.Principal{SubjectID: "user:other"}, &proto.GetAgentRequest{AgentId: created.GetId()})
	if !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetAgent wrong owner error = %v, want not found", err)
	}
}

func TestContractRunEventsAndInteractionRecovery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	provider := newContractTestProvider()
	manager := newContractTestManager(t, provider)
	owner := &principal.Principal{SubjectID: "user:owner"}
	agent, err := manager.CreateAgent(ctx, owner, &proto.CreateAgentRequest{
		Config:         &proto.AgentConfigInput{ProviderName: "managed"},
		IdempotencyKey: "create-1",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	run, err := manager.CreateRun(ctx, owner, &proto.CreateAgentRunRequest{
		AgentId:        agent.GetId(),
		Message:        "Fix the test",
		IdempotencyKey: "run-1",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.GetAgentId() != agent.GetId() || run.GetConfigRevision() != agent.GetConfigRevision() {
		t.Fatalf("CreateRun = %#v", run)
	}
	key := agent.GetId() + "/" + run.GetId()
	provider.events[key] = []*proto.AgentRunEvent{{
		Id:       "event-1",
		Cursor:   "1",
		Sequence: 1,
		AgentId:  agent.GetId(),
		RunId:    run.GetId(),
		Type:     proto.AgentRunEventType_AGENT_RUN_EVENT_TYPE_INTERACTION_REQUESTED,
	}}
	interaction := &proto.AgentRunInteraction{
		Id:      "interaction-1",
		AgentId: agent.GetId(),
		RunId:   run.GetId(),
		Kind:    proto.AgentInteractionKind_AGENT_INTERACTION_KIND_APPROVAL,
		State:   proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING,
		Request: &proto.AgentRunInteraction_Approval{
			Approval: &proto.AgentApprovalInteraction{Action: "run tests"},
		},
	}
	provider.interactions[key+"/"+interaction.GetId()] = interaction

	events, err := manager.ListRunEvents(ctx, owner, &proto.ListAgentRunEventsRequest{
		AgentId: agent.GetId(),
		RunId:   run.GetId(),
	})
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	if len(events.GetEvents()) != 1 || events.GetEvents()[0].GetCursor() != "1" {
		t.Fatalf("ListRunEvents = %#v", events)
	}
	pending, err := manager.ListRunInteractions(ctx, owner, &proto.ListAgentRunInteractionsRequest{
		AgentId: agent.GetId(),
		RunId:   run.GetId(),
		State:   proto.AgentInteractionState_AGENT_INTERACTION_STATE_PENDING,
	})
	if err != nil {
		t.Fatalf("ListRunInteractions: %v", err)
	}
	if len(pending.GetInteractions()) != 1 {
		t.Fatalf("ListRunInteractions = %#v", pending)
	}
	resolved, err := manager.ResolveRunInteraction(ctx, owner, &proto.ResolveAgentRunInteractionRequest{
		AgentId:        agent.GetId(),
		RunId:          run.GetId(),
		InteractionId:  interaction.GetId(),
		IdempotencyKey: "resolve-1",
		Resolution: &proto.AgentInteractionResolution{
			Resolution: &proto.AgentInteractionResolution_Approval{
				Approval: &proto.AgentApprovalResolution{
					Decision: proto.AgentApprovalDecision_AGENT_APPROVAL_DECISION_APPROVE,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ResolveRunInteraction: %v", err)
	}
	if resolved.GetState() != proto.AgentInteractionState_AGENT_INTERACTION_STATE_RESOLVED {
		t.Fatalf("ResolveRunInteraction state = %v", resolved.GetState())
	}
	recovered, err := manager.GetRun(ctx, owner, &proto.GetAgentRunRequest{
		AgentId: agent.GetId(),
		RunId:   run.GetId(),
	})
	if err != nil {
		t.Fatalf("GetRun after recovery: %v", err)
	}
	if recovered.GetId() != run.GetId() {
		t.Fatalf("GetRun = %#v", recovered)
	}
}

var _ coreagent.ContractProvider = (*contractTestProvider)(nil)
