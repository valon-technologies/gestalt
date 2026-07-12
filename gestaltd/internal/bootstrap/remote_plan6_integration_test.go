package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/internal/testutil/fakeremote"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func startPlan6FakeRemote(t *testing.T) *fakeremote.Gestaltd {
	t.Helper()
	return fakeremote.Start(t,
		fakeremote.AppStub("linear", "issues.list"),
		fakeremote.AppStub("valon-profile", "issues.list"),
	)
}

func plan6RemoteRoutingConfig(t *testing.T, localDevActive map[string]bool) *config.Config {
	t.Helper()
	cfg := remoteRoutingConfig(t, localDevActive)
	cfg.Server.Remote = "https://remote.test"
	return cfg
}

func plan6GestaltRemoteBroker(t *testing.T, cfg *config.Config, clients *remote.ClientSet, localApps ...core.Provider) *invocation.Broker {
	t.Helper()
	reg := registry.New()
	for _, provider := range localApps {
		if err := reg.Providers.Register(provider.Name(), provider); err != nil {
			t.Fatalf("Register %q: %v", provider.Name(), err)
		}
	}
	if err := registerRemoteApps(&reg.Providers, cfg, Deps{RemoteClients: clients}); err != nil {
		t.Fatalf("registerRemoteApps: %v", err)
	}
	svc := testutil.NewStubServices(t)
	return invocation.NewBroker(&reg.Providers, svc.Users, svc.ExternalCredentials)
}

func plan6BuildRemoteAgents(t *testing.T, cfg *config.Config, clients *remote.ClientSet) []coreagent.Provider {
	t.Helper()
	agents, _, err := buildAgents(context.Background(), cfg, NewFactoryRegistry(), Deps{RemoteClients: clients})
	if err != nil {
		t.Fatalf("buildAgents: %v", err)
	}
	return agents
}

func TestPlan6RemoteRoutingOverFakeGestaltdLifecycles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		localApps    map[string]bool
		localStubs   []core.Provider
		localChecks  []struct{ app, operation string }
		remoteChecks []struct{ app, operation string }
		wantRemoteN  int
	}{
		{
			name: "nothing local",
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
				{"valon-profile", "issues.list"},
			},
			wantRemoteN: 2,
		},
		{
			name:       "ci-cd local",
			localApps:  map[string]bool{"ci-cd": true},
			localStubs: []core.Provider{localRoutingAppStub("ci-cd")},
			localChecks: []struct{ app, operation string }{
				{"ci-cd", "ping"},
			},
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
				{"valon-profile", "issues.list"},
			},
			wantRemoteN: 2,
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
			localChecks: []struct{ app, operation string }{
				{"valon-profile", "ping"},
			},
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
			},
			wantRemoteN: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := startPlan6FakeRemote(t)
			cfg := plan6RemoteRoutingConfig(t, tc.localApps)
			broker := plan6GestaltRemoteBroker(t, cfg, fake.Clients, tc.localStubs...)

			for _, check := range tc.localChecks {
				result := invokeRemoteRoutingApp(t, broker, check.app, check.operation)
				if result.Status != 201 || string(result.Body) != "local" {
					t.Fatalf("Invoke(%q) = %#v, want local 201", check.app, result)
				}
			}
			for _, check := range tc.remoteChecks {
				result := invokeRemoteRoutingApp(t, broker, check.app, check.operation)
				if result.Status != 202 || string(result.Body) != "relayed" {
					t.Fatalf("Invoke(%q) = %#v, want remote 202 relayed", check.app, result)
				}
			}

			calls := fake.Invoker.Snapshot()
			if len(calls) != tc.wantRemoteN {
				t.Fatalf("remote invocations = %d, want %d (%#v)", len(calls), tc.wantRemoteN, calls)
			}
			tokens := fake.BearerRecorder.Snapshot()
			if len(tokens) == 0 {
				t.Fatal("remote fake received no bearer tokens")
			}
			if tokens[0] != fake.Token {
				t.Fatalf("first bearer token = %q, want %q", tokens[0], fake.Token)
			}
		})
	}
}

func TestPlan6RemoteAgentRoutesOverFakeGestaltd(t *testing.T) {
	t.Parallel()

	fake := startPlan6FakeRemote(t)
	cfg := &config.Config{
		Server: config.ServerConfig{Remote: fake.URL},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
	}
	if providerBuildsLocal(cfg, cfg.Providers.Agent["managed"]) {
		t.Fatal("configured remote agent provider should not build local")
	}
	agents := plan6BuildRemoteAgents(t, cfg, fake.Clients)
	if len(agents) != 1 {
		t.Fatalf("agents = %d, want 1 remote agent provider", len(agents))
	}
}

func TestPlan6RemoteRoutingFailureSemanticsOverFakeGestaltd(t *testing.T) {
	t.Parallel()

	fake := startPlan6FakeRemote(t)

	t.Run("undeclared provider remains not found", func(t *testing.T) {
		t.Parallel()
		cfg := plan6RemoteRoutingConfig(t, nil)
		broker := plan6GestaltRemoteBroker(t, cfg, fake.Clients)
		_, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("missing"), "missing", "", "op", nil)
		if !errors.Is(err, invocation.ErrProviderNotFound) {
			t.Fatalf("err = %v, want %v", err, invocation.ErrProviderNotFound)
		}
		if len(fake.Invoker.Snapshot()) != 0 {
			t.Fatal("undeclared provider reached remote fake")
		}
	})

	t.Run("dev active does not fall back to remote", func(t *testing.T) {
		t.Parallel()
		cfg := plan6RemoteRoutingConfig(t, map[string]bool{"linear": true})
		broker := plan6GestaltRemoteBroker(t, cfg, fake.Clients)
		_, err := broker.Invoke(context.Background(), remoteRoutingPrincipal("linear"), "linear", "", "issues.list", nil)
		if !errors.Is(err, invocation.ErrProviderNotFound) {
			t.Fatalf("err = %v, want %v", err, invocation.ErrProviderNotFound)
		}
	})

	t.Run("local-only when server.remote is empty", func(t *testing.T) {
		t.Parallel()
		cfg := remoteRoutingConfig(t, nil)
		cfg.Server.Remote = ""
		reg := registry.New()
		if err := registerRemoteApps(&reg.Providers, cfg, Deps{RemoteClients: fake.Clients}); err != nil {
			t.Fatalf("registerRemoteApps with empty remote: %v", err)
		}
		if len(reg.Providers.List()) != 0 {
			t.Fatalf("providers = %d, want none when server.remote is empty", len(reg.Providers.List()))
		}
	})
}

func TestPlan6LocalProviderStartupFailureDoesNotRouteRemote(t *testing.T) {
	t.Parallel()

	fake := startPlan6FakeRemote(t)
	cfg := plan6RemoteRoutingConfig(t, map[string]bool{"linear": true})
	builds, err := prepareProviderBuilds(cfg, NewFactoryRegistry(), Deps{RemoteClients: fake.Clients})
	if err != nil {
		t.Fatalf("prepareProviderBuilds: %v", err)
	}

	boom := errors.New("local startup failed")
	ready, _, _, errResolver := builds.Start(context.Background(), Deps{RemoteClients: fake.Clients}, func(context.Context, string, *config.ProviderEntry, Deps) (*ProviderBuildResult, error) {
		return nil, boom
	})
	<-ready
	if errs := errResolver(); len(errs) == 0 || !errors.Is(errs[0], boom) {
		t.Fatalf("startup errors = %v, want %v", errs, boom)
	}

	reg := registry.New()
	if err := registerRemoteApps(&reg.Providers, cfg, Deps{RemoteClients: fake.Clients}); err != nil {
		t.Fatalf("registerRemoteApps: %v", err)
	}
	broker := invocation.NewBroker(&reg.Providers, testutil.NewStubServices(t).Users, testutil.NewStubServices(t).ExternalCredentials)
	_, err = broker.Invoke(context.Background(), remoteRoutingPrincipal("linear"), "linear", "", "issues.list", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("err = %v, want %v after local startup failure", err, invocation.ErrProviderNotFound)
	}
	if len(fake.Invoker.Snapshot()) != 0 {
		t.Fatal("local startup failure fell back to remote routing")
	}
}

func TestPlan6GestaltRemotePreservesRemoteCredentialDelegation(t *testing.T) {
	t.Parallel()

	fake := startPlan6FakeRemote(t)
	spec, _, err := buildStartupProviderSpec("linear", remoteRoutingAppEntry(t, "linear", "issues.list"))
	if err != nil {
		t.Fatalf("buildStartupProviderSpec: %v", err)
	}
	provider := appservice.NewGestaltRemote(fake.Clients.App, spec)
	delegated, ok := provider.(interface{ RemoteCredentialDelegated() bool })
	if !ok || !delegated.RemoteCredentialDelegated() {
		t.Fatal("gestalt remote provider should delegate remote credentials")
	}
}

func TestPlan6RemoteRoutingPrincipalScope(t *testing.T) {
	t.Parallel()

	p := remoteRoutingPrincipal("linear")
	if p == nil || p.SubjectID == "" {
		t.Fatalf("principal = %#v, want non-empty subject", p)
	}
	if len(p.Scopes) != 1 || p.Scopes[0] != "linear" {
		t.Fatalf("principal scopes = %#v, want [linear]", p.Scopes)
	}
}
