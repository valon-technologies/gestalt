package bootstrap

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"gopkg.in/yaml.v3"
)

type recordingPublicAgentClient struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *recordingPublicAgentClient) CreateSession(context.Context, *proto.CreateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return &proto.AgentSession{Id: "remote-session"}, nil
}

func (c *recordingPublicAgentClient) GetSession(context.Context, *proto.GetAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderSessionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderInteractionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicAgentClient) snapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type recordingPublicWorkflowClient struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *recordingPublicWorkflowClient) SetDefinitionPaused(context.Context, *proto.SetWorkflowProviderDefinitionPausedRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) SetActivationPaused(context.Context, *proto.SetWorkflowProviderActivationPausedRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) DeleteDefinition(context.Context, *proto.DeleteWorkflowProviderDefinitionRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) ApplyDefinition(context.Context, *proto.ApplyWorkflowProviderDefinitionRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) GetDefinition(context.Context, *proto.GetWorkflowProviderDefinitionRequest, ...grpc.CallOption) (*proto.WorkflowDefinition, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) ListDefinitions(context.Context, *proto.ListWorkflowProviderDefinitionsRequest, ...grpc.CallOption) (*proto.ListWorkflowProviderDefinitionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) StartRun(ctx context.Context, _ *proto.StartWorkflowProviderRunRequest, _ ...grpc.CallOption) (*proto.WorkflowRun, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return &proto.WorkflowRun{Id: "remote-run"}, nil
}

func (c *recordingPublicWorkflowClient) ListRuns(context.Context, *proto.ListWorkflowProviderRunsRequest, ...grpc.CallOption) (*proto.ListWorkflowProviderRunsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) GetRun(context.Context, *proto.GetWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.WorkflowRun, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) CancelRun(context.Context, *proto.CancelWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.WorkflowRun, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) GetRunEvents(context.Context, *proto.GetWorkflowProviderRunEventsRequest, ...grpc.CallOption) (*proto.GetWorkflowProviderRunEventsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) GetRunOutput(context.Context, *proto.GetWorkflowProviderRunOutputRequest, ...grpc.CallOption) (*proto.GetWorkflowProviderRunOutputResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) SignalRun(context.Context, *proto.SignalWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.SignalWorkflowRunResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) SignalOrStartRun(context.Context, *proto.SignalOrStartWorkflowProviderRunRequest, ...grpc.CallOption) (*proto.SignalWorkflowRunResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) DeliverEvent(context.Context, *proto.DeliverWorkflowProviderEventRequest, ...grpc.CallOption) (*proto.WorkflowEvent, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingPublicWorkflowClient) snapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func remoteHostServiceConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Remote: "https://remote.test"},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {Source: config.ProviderSource{Path: "stub"}},
			},
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
	}
}

func TestRemoteHostServiceProvidersRouteThroughPublicGRPC(t *testing.T) {
	t.Parallel()

	t.Run("agent create session routes remote", func(t *testing.T) {
		t.Parallel()

		agentClient := &recordingPublicAgentClient{}
		cfg := remoteHostServiceConfig()
		deps := Deps{RemoteClients: &remote.ClientSet{Agent: agentClient}}
		provider, err := buildAgent(context.Background(), cfg, "managed", cfg.Providers.Agent["managed"], NewFactoryRegistry(), deps)
		if err != nil {
			t.Fatalf("buildAgent: %v", err)
		}
		session, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{Model: "gpt-test"})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if session == nil || session.ID != "remote-session" {
			t.Fatalf("session = %#v, want remote-session", session)
		}
		if got := agentClient.snapshot(); got != 1 {
			t.Fatalf("remote agent calls = %d, want 1", got)
		}
	})

	t.Run("workflow start run routes remote", func(t *testing.T) {
		t.Parallel()

		workflowClient := &recordingPublicWorkflowClient{}
		cfg := remoteHostServiceConfig()
		deps := Deps{RemoteClients: &remote.ClientSet{Workflow: workflowClient}}
		provider, err := buildWorkflow(context.Background(), cfg, "temporal", cfg.Providers.Workflow["temporal"], NewFactoryRegistry(), deps)
		if err != nil {
			t.Fatalf("buildWorkflow: %v", err)
		}
		run, err := provider.StartRun(context.Background(), &proto.StartWorkflowProviderRunRequest{})
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if run == nil || run.Id != "remote-run" {
			t.Fatalf("run = %#v, want remote-run", run)
		}
		if got := workflowClient.snapshot(); got != 1 {
			t.Fatalf("remote workflow calls = %d, want 1", got)
		}
	})
}

func TestRemoteRoutingPreservesLocalOnlyBehaviorWithoutRemoteURL(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
	}
	if !providerBuildsLocal(cfg, cfg.Providers.Agent["managed"]) {
		t.Fatal("agent provider should build locally when server.remote is empty")
	}
	factories := NewFactoryRegistry()
	factories.Agent = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (coreagent.Provider, error) {
		return providerBuildOrderingAgentProvider{}, nil
	}
	if _, err := buildAgent(context.Background(), cfg, "managed", cfg.Providers.Agent["managed"], factories, Deps{}); err != nil {
		t.Fatalf("buildAgent without remote: %v", err)
	}
}

func TestRemoteAppRoutingLocalStartupFailureDoesNotFallBackToRemote(t *testing.T) {
	t.Parallel()

	var remoteCalls atomic.Int32
	remoteClient := &countingRemoteAppClient{callback: func() { remoteCalls.Add(1) }}
	cfg := remoteRoutingConfig(t, map[string]bool{"linear": true})
	broker := newRemoteRoutingBroker(t, cfg, remoteClient)

	_, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("linear"), "linear", "", "issues.list", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("err = %v, want %v", err, invocation.ErrProviderNotFound)
	}
	if got := remoteCalls.Load(); got != 0 {
		t.Fatalf("remote client calls = %d, want 0 when local DevActive provider is missing", got)
	}
}

type countingRemoteAppClient struct {
	recordingRemoteAppClient
	callback func()
}

func (c *countingRemoteAppClient) Invoke(ctx context.Context, req *proto.AppInvokeRequest, opts ...grpc.CallOption) (*proto.OperationResult, error) {
	if c.callback != nil {
		c.callback()
	}
	return c.recordingRemoteAppClient.Invoke(ctx, req, opts...)
}
