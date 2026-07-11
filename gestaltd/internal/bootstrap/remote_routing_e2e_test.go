package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recordingRemoteAgentClient struct {
	mu           sync.Mutex
	sessionCalls int
	providerName string
	err          error
}

func (c *recordingRemoteAgentClient) CreateSession(_ context.Context, req *proto.CreateAgentProviderSessionRequest, _ ...grpc.CallOption) (*proto.AgentSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	c.sessionCalls++
	c.providerName = req.GetProviderName()
	return &proto.AgentSession{Id: "remote-session-1", ProviderName: req.GetProviderName()}, nil
}

func (c *recordingRemoteAgentClient) GetSession(context.Context, *proto.GetAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) ListSessions(context.Context, *proto.ListAgentProviderSessionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderSessionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) UpdateSession(context.Context, *proto.UpdateAgentProviderSessionRequest, ...grpc.CallOption) (*proto.AgentSession, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) CreateTurn(context.Context, *proto.CreateAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) GetTurn(context.Context, *proto.GetAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) ListTurns(context.Context, *proto.ListAgentProviderTurnsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) CancelTurn(context.Context, *proto.CancelAgentProviderTurnRequest, ...grpc.CallOption) (*proto.AgentTurn, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) ListTurnEvents(context.Context, *proto.ListAgentProviderTurnEventsRequest, ...grpc.CallOption) (*proto.ListAgentProviderTurnEventsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) GetInteraction(context.Context, *proto.GetAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) ListInteractions(context.Context, *proto.ListAgentProviderInteractionsRequest, ...grpc.CallOption) (*proto.ListAgentProviderInteractionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) ResolveInteraction(context.Context, *proto.ResolveAgentProviderInteractionRequest, ...grpc.CallOption) (*proto.AgentInteraction, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) GetCapabilities(context.Context, *proto.GetAgentProviderCapabilitiesRequest, ...grpc.CallOption) (*proto.AgentProviderCapabilities, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (c *recordingRemoteAgentClient) snapshot() (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionCalls, c.providerName
}

func remoteRoutingConfigWithAgent(t *testing.T, localDevActive map[string]bool) *config.Config {
	t.Helper()
	cfg := remoteRoutingConfig(t, localDevActive)
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"managed": {Source: config.ProviderSource{Path: "stub"}},
	}
	return cfg
}

func buildRemoteRoutingAgents(t *testing.T, cfg *config.Config, remoteAgent proto.AgentClient) []coreagent.Provider {
	t.Helper()
	agents, _, err := buildAgents(context.Background(), cfg, NewFactoryRegistry(), Deps{
		RemoteClients: &remote.ClientSet{App: &recordingRemoteAppClient{}, Agent: remoteAgent},
	})
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	return agents
}

func invokeRemoteRoutingAgent(t *testing.T, provider coreagent.Provider) {
	t.Helper()
	_, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

func TestRemoteRoutingAgentLifecycles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		localApps    map[string]bool
		localStubs   []core.Provider
		wantSessions int
	}{
		{name: "nothing local", wantSessions: 1},
		{
			name:       "ci-cd local",
			localApps:  map[string]bool{"ci-cd": true},
			localStubs: []core.Provider{localRoutingAppStub("ci-cd")},
			wantSessions: 1,
		},
		{
			name: "ci-cd and valon-profile local",
			localApps: map[string]bool{
				"ci-cd":         true,
				"valon-profile": true,
			},
			localStubs: []core.Provider{
				localRoutingAppStub("ci-cd"),
				localRoutingAppStub("valon-profile"),
			},
			wantSessions: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			remoteAgent := &recordingRemoteAgentClient{}
			cfg := remoteRoutingConfigWithAgent(t, tc.localApps)
			agents := buildRemoteRoutingAgents(t, cfg, remoteAgent)
			if len(agents) != 1 {
				t.Fatalf("agents = %d, want 1 remote agent provider", len(agents))
			}

			invokeRemoteRoutingAgent(t, agents[0])

			calls, providerName := remoteAgent.snapshot()
			if calls != tc.wantSessions {
				t.Fatalf("remote agent session calls = %d, want %d", calls, tc.wantSessions)
			}
			if providerName != "managed" {
				t.Fatalf("remote agent provider name = %q, want managed", providerName)
			}

			remoteClient := &recordingRemoteAppClient{}
			broker := newRemoteRoutingBroker(t, cfg, remoteClient, tc.localStubs...)
			for _, check := range []struct{ app, operation string }{
				{"linear", "issues.list"},
			} {
				if tc.name == "ci-cd and valon-profile local" {
					continue
				}
				invokeRemoteRoutingApp(t, broker, check.app, check.operation)
			}
			if tc.name == "ci-cd and valon-profile local" {
				invokeRemoteRoutingApp(t, broker, "linear", "issues.list")
				if got := remoteClient.snapshot(); len(got) != 1 || got[0].app != "linear" {
					t.Fatalf("remote app calls = %#v, want linear only", got)
				}
				result := invokeRemoteRoutingApp(t, broker, "valon-profile", "ping")
				if result.Status != 201 || string(result.Body) != "local" {
					t.Fatalf("Invoke(valon-profile) = %#v, want local 201", result)
				}
				return
			}
			if tc.name == "ci-cd local" {
				result := invokeRemoteRoutingApp(t, broker, "ci-cd", "ping")
				if result.Status != 201 || string(result.Body) != "local" {
					t.Fatalf("Invoke(ci-cd) = %#v, want local 201", result)
				}
			}
		})
	}
}

func TestLocalDevActiveStartupFailureDoesNotRouteRemote(t *testing.T) {
	t.Parallel()

	remoteClient := &recordingRemoteAppClient{}
	cfg := remoteRoutingConfig(t, map[string]bool{"ci-cd": true})
	builds, err := prepareProviderBuilds(cfg, NewFactoryRegistry(), Deps{
		RemoteClients: &remote.ClientSet{App: remoteClient},
	})
	if err != nil {
		t.Fatalf("prepareProviderBuilds: %v", err)
	}

	boom := errors.New("local build failed")
	ready, _, _, errResolver := builds.Start(context.Background(), Deps{}, func(_ context.Context, name string, _ *config.ProviderEntry, _ Deps) (*ProviderBuildResult, error) {
		if name == "ci-cd" {
			return nil, boom
		}
		return nil, fmt.Errorf("unexpected provider %q", name)
	})
	<-ready
	if errs := errResolver(); len(errs) == 0 {
		t.Fatal("provider build errors = nil, want local startup failure")
	}

	svc := testutil.NewStubServices(t)
	broker := invocation.NewBroker(builds.providers, svc.Users, svc.ExternalCredentials)
	_, err = broker.Invoke(context.Background(), remoteRoutingPrincipal("ci-cd"), "ci-cd", "", "ping", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("Invoke(ci-cd) err = %v, want provider not found without remote fallback", err)
	}

	result, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("linear"), "linear", "", "issues.list", nil)
	if err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	if result.Status != 202 || string(result.Body) != "relayed" {
		t.Fatalf("Invoke(linear) = %#v, want remote relay", result)
	}
	if len(remoteClient.snapshot()) != 1 {
		t.Fatalf("remote client calls = %d, want 1 for linear only", len(remoteClient.snapshot()))
	}
}
