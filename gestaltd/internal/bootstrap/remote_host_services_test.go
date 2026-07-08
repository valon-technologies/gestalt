package bootstrap

import (
	"context"
	"errors"
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	agentservice "github.com/valon-technologies/gestalt/server/services/agents"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
	workflowservice "github.com/valon-technologies/gestalt/server/services/workflows"
)

func TestRegisterRemoteAgentsSkipsLocalProviders(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{Remote: "https://valon.tools", RemoteToken: "token"},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {DevActive: true},
				"remote":  {},
			},
		},
	}
	agentRuntime, err := newAgentRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	local := &stubAgentProvider{name: "managed"}
	agentRuntime.PublishProvider("managed", local)

	clients := &remote.ClientSet{Agent: &remoteAgentStub{}}
	registerRemoteAgents(cfg, Deps{
		Placement:     NewPlacementPlan(cfg),
		AgentRuntime:  agentRuntime,
		RemoteClients: clients,
	})

	if _, _, err := agentRuntime.ResolveProvider(context.Background(), "managed"); err != nil {
		t.Fatalf("local managed provider missing: %v", err)
	}
	_, remoteProvider, err := agentRuntime.ResolveProvider(context.Background(), "remote")
	if err != nil {
		t.Fatalf("remote provider missing: %v", err)
	}
	if remoteProvider == local {
		t.Fatal("expected distinct remote provider")
	}
}

func TestRegisterRemoteWorkflowsSkipsLocalProviders(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{Remote: "https://valon.tools", RemoteToken: "token"},
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {DevActive: true},
				"remote":   {},
			},
		},
	}
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	local := &stubWorkflowProvider{name: "temporal"}
	workflowRuntime.PublishProvider("temporal", local)

	clients := &remote.ClientSet{Workflow: &remoteWorkflowStub{}}
	registerRemoteWorkflows(cfg, Deps{
		Placement:       NewPlacementPlan(cfg),
		WorkflowRuntime: workflowRuntime,
		RemoteClients:   clients,
	})

	if _, _, err := workflowRuntime.ResolveProvider(context.Background(), "temporal"); err != nil {
		t.Fatalf("local temporal provider missing: %v", err)
	}
	_, remoteProvider, err := workflowRuntime.ResolveProvider(context.Background(), "remote")
	if err != nil {
		t.Fatalf("remote provider missing: %v", err)
	}
	if remoteProvider == local {
		t.Fatal("expected distinct remote provider")
	}
}

func TestRemoteIndexedDBBindingsSkipsLocal(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{Remote: "https://valon.tools", RemoteToken: "token"},
		Providers: config.ProvidersConfig{
			IndexedDB: map[string]*config.ProviderEntry{
				"system": {DevActive: true},
				"remote": {},
			},
		},
	}
	placement := NewPlacementPlan(cfg)
	clients := &remote.ClientSet{IndexedDB: &remoteIndexedDBStub{}}
	bindings := remoteIndexedDBBindings(cfg, placement, clients, "system")
	if len(bindings) != 1 {
		t.Fatalf("bindings = %d, want 1", len(bindings))
	}
	if bindings["remote"] == nil {
		t.Fatal("expected remote indexeddb binding")
	}
	if bindings["system"] != nil {
		t.Fatal("system binding should be excluded")
	}
}

type stubAgentProvider struct {
	name string
}

func (s *stubAgentProvider) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) GetSession(context.Context, *proto.GetAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest) ([]*coreagent.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest) (*coreagent.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest) ([]*coreagent.Turn, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest) (*coreagent.Turn, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest) ([]*coreagent.TurnEvent, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest) ([]*coreagent.Interaction, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest) (*coreagent.Interaction, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest) (*coreagent.ProviderCapabilities, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgentProvider) Ping(context.Context) error { return nil }
func (s *stubAgentProvider) Close() error               { return nil }

type stubWorkflowProvider struct {
	name string
}

func (s *stubWorkflowProvider) ApplyDefinition(context.Context, *proto.ApplyWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) ListDefinitions(context.Context, *proto.ListWorkflowProviderDefinitionsRequest) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) SetDefinitionPaused(context.Context, *proto.SetWorkflowProviderDefinitionPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) SetActivationPaused(context.Context, *proto.SetWorkflowProviderActivationPausedRequest) (*proto.WorkflowDefinition, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest) error {
	return errors.New("not implemented")
}
func (s *stubWorkflowProvider) StartRun(context.Context, *proto.StartWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest) (*proto.ListWorkflowProviderRunsResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest) (*proto.WorkflowRun, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest) (*proto.SignalWorkflowRunResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) DeliverEvent(context.Context, *proto.DeliverWorkflowProviderEventRequest) (*proto.WorkflowEvent, error) {
	return nil, errors.New("not implemented")
}
func (s *stubWorkflowProvider) Ping(context.Context) error { return nil }
func (s *stubWorkflowProvider) Close() error               { return nil }

type remoteAgentStub struct{ proto.AgentClient }
type remoteWorkflowStub struct{ proto.WorkflowClient }
type remoteIndexedDBStub struct{ proto.IndexedDBClient }

var (
	_ coreagent.Provider    = agentservice.NewGestaltRemoteProvider(&remoteAgentStub{}, "remote")
	_ coreworkflow.Provider = workflowservice.NewGestaltRemoteProvider(&remoteWorkflowStub{}, "remote")
	_                       = indexeddbservice.NewGestaltRemoteProvider(&remoteIndexedDBStub{})
)
