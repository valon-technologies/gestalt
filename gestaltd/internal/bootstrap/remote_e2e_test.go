package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/remotetest"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func remoteE2ERequestContext(ctx context.Context, _ string) (*proto.RequestContext, error) {
	p := principal.FromContext(ctx)
	if p == nil {
		return nil, nil
	}
	return &proto.RequestContext{
		Subject: &proto.SubjectContext{Id: p.SubjectID},
	}, nil
}

func plan6RemoteConfig(fake *remotetest.Server, localApps map[string]bool) *config.Config {
	apps := map[string]*config.ProviderEntry{
		"linear":        {},
		"valon-profile": {},
		"ci-cd":         {},
	}
	for name, active := range localApps {
		if entry := apps[name]; entry != nil {
			entry.DevActive = active
		}
	}
	return &config.Config{
		Server: config.ServerConfig{
			Remote:      fake.URL(),
			RemoteToken: fake.Token,
			BaseURL:     "http://127.0.0.1:8080",
			Providers: config.ServerProvidersConfig{
				IndexedDB: "inmem",
			},
		},
		Apps: apps,
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"managed": {},
			},
			Workflow: map[string]*config.ProviderEntry{
				"default": {},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"inmem":   {Source: config.ProviderSource{Builtin: "memory"}},
				"archive": {},
			},
		},
	}
}

func localAppStub(name string) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: "ping"}},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 201, Body: []byte("local")}, nil
		},
	}
}

func newRemoteBroker(
	t *testing.T,
	cfg *config.Config,
	fake *remotetest.Server,
	localApps ...core.Provider,
) *invocation.Broker {
	t.Helper()

	plan := NewPlacementPlan(cfg)
	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	svc := testutil.NewStubServices(t)
	return invocation.NewBroker(
		testutil.NewProviderRegistry(t, localApps...),
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithRemoteAppRouting(
			func(name string) bool {
				return plan.ShouldRouteRemote(RemoteProviderKindApp, name)
			},
			clients.App,
			cfg.Server.BaseURL,
			remoteE2ERequestContext,
		),
	)
}

func testPrincipal(scopes ...string) *principal.Principal {
	return &principal.Principal{
		SubjectID: "user:dev@example.com",
		Kind:      principal.KindUser,
		Scopes:    scopes,
	}
}

func TestPlan6RemoteLifecycleNothingLocal(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, nil)
	broker := newRemoteBroker(t, cfg, fake)

	for _, app := range []string{"linear", "valon-profile"} {
		result, err := broker.Invoke(context.Background(), testPrincipal(app), app, "", "issues.list", nil)
		if err != nil {
			t.Fatalf("Invoke(%q): %v", app, err)
		}
		if result == nil || result.Status != 200 || string(result.Body) != `{"remote":true}` {
			t.Fatalf("Invoke(%q) = %#v, want remote 200", app, result)
		}
	}

	deps := Deps{Placement: NewPlacementPlan(cfg)}
	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clients.Close() }()
	deps.RemoteClients = clients

	agentRuntime, err := newAgentRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	deps.AgentRuntime = agentRuntime
	workflowRuntime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	deps.WorkflowRuntime = workflowRuntime
	deps.IndexedDBs = map[string]indexeddb.IndexedDB{}

	if _, err := publishRemoteProviders(context.Background(), cfg, &deps); err != nil {
		t.Fatalf("publishRemoteProviders: %v", err)
	}

	_, provider, err := agentRuntime.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider(agent): %v", err)
	}
	if _, err := provider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, wfProvider, err := workflowRuntime.ResolveProvider(context.Background(), "default")
	if err != nil {
		t.Fatalf("ResolveProvider(workflow): %v", err)
	}
	if _, err := wfProvider.StartRun(context.Background(), &proto.StartWorkflowProviderRunRequest{}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	store, ok := deps.IndexedDBs["archive"]
	if !ok {
		t.Fatal("expected remote indexeddb archive store")
	}
	if _, err := store.ObjectStore("items").Get(context.Background(), "k1"); err != nil {
		t.Fatalf("IndexedDB Get: %v", err)
	}

	appInvokes := fake.Recorder.AppInvokesSnapshot()
	if len(appInvokes) != 2 {
		t.Fatalf("remote app invokes = %d, want 2", len(appInvokes))
	}
	for _, rec := range appInvokes {
		if rec.Authorization != "Bearer "+fake.Token {
			t.Fatalf("authorization = %q, want bearer token", rec.Authorization)
		}
	}
	agentCreates := fake.Recorder.AgentCreatesSnapshot()
	if len(agentCreates) != 1 || agentCreates[0].ProviderName != "managed" {
		t.Fatalf("agent creates = %#v", agentCreates)
	}
	workflowStarts := fake.Recorder.WorkflowStartsSnapshot()
	if len(workflowStarts) != 1 || workflowStarts[0].ProviderName != "default" {
		t.Fatalf("workflow starts = %#v", workflowStarts)
	}
	indexedDBGets := fake.Recorder.IndexedDBGetsSnapshot()
	if len(indexedDBGets) != 1 || indexedDBGets[0].HostBinding != "archive" {
		t.Fatalf("indexeddb gets = %#v", indexedDBGets)
	}
}

func TestPlan6RemoteLifecycleCICDLocal(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, map[string]bool{"ci-cd": true})
	broker := newRemoteBroker(t, cfg, fake, localAppStub("ci-cd"))

	result, err := broker.Invoke(context.Background(), testPrincipal("ci-cd"), "ci-cd", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke(ci-cd): %v", err)
	}
	if result == nil || result.Status != 201 || string(result.Body) != "local" {
		t.Fatalf("Invoke(ci-cd) = %#v, want local 201", result)
	}

	for _, app := range []string{"linear", "valon-profile"} {
		result, err := broker.Invoke(context.Background(), testPrincipal(app), app, "", "issues.list", nil)
		if err != nil {
			t.Fatalf("Invoke(%q): %v", app, err)
		}
		if result == nil || result.Status != 200 {
			t.Fatalf("Invoke(%q) = %#v, want remote 200", app, result)
		}
	}

	appInvokes := fake.Recorder.AppInvokesSnapshot()
	if len(appInvokes) != 2 {
		t.Fatalf("remote app invokes = %d, want 2", len(appInvokes))
	}
}

func TestPlan6RemoteLifecycleCICDAndProfileLocal(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, map[string]bool{"ci-cd": true, "valon-profile": true})
	broker := newRemoteBroker(t, cfg, fake, localAppStub("ci-cd"), localAppStub("valon-profile"))

	result, err := broker.Invoke(context.Background(), testPrincipal("valon-profile"), "valon-profile", "", "ping", nil)
	if err != nil {
		t.Fatalf("Invoke(valon-profile): %v", err)
	}
	if result == nil || result.Status != 201 {
		t.Fatalf("Invoke(valon-profile) = %#v, want local 201", result)
	}

	result, err = broker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "issues.list", nil)
	if err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	if result == nil || result.Status != 200 {
		t.Fatalf("Invoke(linear) = %#v, want remote 200", result)
	}

	appInvokes := fake.Recorder.AppInvokesSnapshot()
	if len(appInvokes) != 1 || appInvokes[0].App != "linear" {
		t.Fatalf("remote app invokes = %#v, want linear only", appInvokes)
	}
}

func TestPlan6UndeclaredProviderRemainsNotFound(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, nil)
	broker := newRemoteBroker(t, cfg, fake)

	_, err := broker.Invoke(context.Background(), testPrincipal("missing"), "missing", "", "op", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound", err)
	}
}

func TestPlan6LocalDevActiveDoesNotFallbackToRemote(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, map[string]bool{"linear": true})
	broker := newRemoteBroker(t, cfg, fake)

	_, err := broker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "issues.list", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound without local provider", err)
	}

	if len(fake.Recorder.AppInvokesSnapshot()) != 0 {
		t.Fatalf("remote app invokes = %d, want 0", len(fake.Recorder.AppInvokesSnapshot()))
	}
}

func TestPlan6RemoteAuthFailureSurfaces(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, nil)

	plan := NewPlacementPlan(cfg)
	cfg.Server.RemoteToken = "wrong-token"
	clients, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   fake.URL(),
		Token: cfg.Server.RemoteToken,
	})
	if err != nil {
		t.Fatalf("NewClientSet wrong token: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	svc := testutil.NewStubServices(t)
	broker := invocation.NewBroker(
		testutil.NewProviderRegistry(t),
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithRemoteAppRouting(
			func(name string) bool {
				return plan.ShouldRouteRemote(RemoteProviderKindApp, name)
			},
			clients.App,
			cfg.Server.BaseURL,
			remoteE2ERequestContext,
		),
	)

	_, err = broker.Invoke(context.Background(), testPrincipal("linear"), "linear", "", "issues.list", nil)
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	if !errors.Is(err, invocation.ErrNotAuthenticated) {
		t.Fatalf("err = %v, want ErrNotAuthenticated", err)
	}
}

func TestPlan6LocalStartupFailureDoesNotPublishRemoteFallback(t *testing.T) {
	t.Parallel()

	cfg := plan6RemoteConfig(remotetest.New(t, remotetest.DefaultToken), map[string]bool{"linear": true})
	agentRuntime, err := newAgentRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	agentRuntime.FailStartupProvider("managed", errors.New("local agent failed"))
	if _, _, err := agentRuntime.ResolveProvider(context.Background(), "managed"); err == nil {
		t.Fatal("expected local startup failure to block provider resolution")
	}
}
