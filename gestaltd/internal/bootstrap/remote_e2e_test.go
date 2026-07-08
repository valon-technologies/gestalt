package bootstrap

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
)

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

func registerPlan6RemoteApps(reg *registry.ProviderMap[core.Provider], cfg *config.Config, client proto.AppClient) error {
	for name, entry := range cfg.Apps {
		if entry == nil || providerRunsLocally(cfg, entry) {
			continue
		}
		spec := appservice.StaticProviderSpec{
			Name:           name,
			ConnectionMode: core.ConnectionModeNone,
			Catalog:        &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "issues.list"}, {ID: "ping"}}},
		}
		provider := appservice.NewGestaltRemote(client, spec)
		if provider == nil {
			return fmt.Errorf("remote app %q: provider client is required", name)
		}
		if err := reg.Register(name, provider); err != nil {
			return fmt.Errorf("remote app %q: %w", name, err)
		}
	}
	return nil
}

func newPlan6Broker(t *testing.T, cfg *config.Config, fake *remotetest.Server, localApps ...core.Provider) *invocation.Broker {
	t.Helper()

	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })

	reg := testutil.NewProviderRegistry(t, localApps...)
	if err := registerPlan6RemoteApps(reg, cfg, clients.App); err != nil {
		t.Fatalf("registerPlan6RemoteApps: %v", err)
	}
	svc := testutil.NewStubServices(t)
	return invocation.NewBroker(reg, svc.Users, svc.ExternalCredentials)
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
	broker := newPlan6Broker(t, cfg, fake)

	for _, app := range []string{"linear", "valon-profile"} {
		result, err := broker.Invoke(context.Background(), testPrincipal(app), app, "", "issues.list", nil)
		if err != nil {
			t.Fatalf("Invoke(%q): %v", app, err)
		}
		if result == nil || result.Status != 200 || string(result.Body) != `{"remote":true}` {
			t.Fatalf("Invoke(%q) = %#v, want remote 200", app, result)
		}
	}

	clients, err := fake.NewClientSet(context.Background())
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clients.Close() }()

	deps := Deps{
		RemoteClients:   clients,
		IndexedDBs:      map[string]indexeddb.IndexedDB{},
		AgentRuntime:    mustAgentRuntime(t, cfg),
		WorkflowRuntime: mustWorkflowRuntime(t, cfg),
	}
	if _, err := publishRemoteProviders(context.Background(), cfg, &deps); err != nil {
		t.Fatalf("publishRemoteProviders: %v", err)
	}

	_, agentProvider, err := deps.AgentRuntime.ResolveProvider(context.Background(), "managed")
	if err != nil {
		t.Fatalf("ResolveProvider(agent): %v", err)
	}
	if _, err := agentProvider.CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, wfProvider, err := deps.WorkflowRuntime.ResolveProvider(context.Background(), "default")
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

	assertBearerAuth(t, fake, 2, 1, 1, 1)
}

func TestPlan6RemoteLifecycleCICDLocal(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, map[string]bool{"ci-cd": true})
	broker := newPlan6Broker(t, cfg, fake, localAppStub("ci-cd"))

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

	if len(fake.Recorder.AppInvokesSnapshot()) != 2 {
		t.Fatalf("remote app invokes = %d, want 2", len(fake.Recorder.AppInvokesSnapshot()))
	}
}

func TestPlan6RemoteLifecycleCICDAndProfileLocal(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, map[string]bool{"ci-cd": true, "valon-profile": true})
	broker := newPlan6Broker(t, cfg, fake, localAppStub("ci-cd"), localAppStub("valon-profile"))

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
	broker := newPlan6Broker(t, plan6RemoteConfig(fake, nil), fake)

	_, err := broker.Invoke(context.Background(), testPrincipal("missing"), "missing", "", "op", nil)
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("err = %v, want ErrProviderNotFound", err)
	}
}

func TestPlan6LocalDevActiveDoesNotFallbackToRemote(t *testing.T) {
	t.Parallel()

	fake := remotetest.New(t, remotetest.DefaultToken)
	cfg := plan6RemoteConfig(fake, map[string]bool{"linear": true})
	broker := newPlan6Broker(t, cfg, fake)

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

	spec := appservice.StaticProviderSpec{
		Name:           "linear",
		ConnectionMode: core.ConnectionModeNone,
		Catalog:        &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "issues.list"}}},
	}
	wrongClients, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   fake.URL(),
		Token: "wrong-token",
	})
	if err != nil {
		t.Fatalf("NewClientSet wrong token: %v", err)
	}
	t.Cleanup(func() { _ = wrongClients.Close() })

	reg := testutil.NewProviderRegistry(t)
	if err := reg.Register("linear", appservice.NewGestaltRemote(wrongClients.App, spec)); err != nil {
		t.Fatalf("Register linear: %v", err)
	}

	svc := testutil.NewStubServices(t)
	broker := invocation.NewBroker(reg, svc.Users, svc.ExternalCredentials)

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

func mustAgentRuntime(t *testing.T, cfg *config.Config) *agentRuntime {
	t.Helper()
	runtime, err := newAgentRuntime(cfg, nil)
	if err != nil {
		t.Fatalf("newAgentRuntime: %v", err)
	}
	return runtime
}

func mustWorkflowRuntime(t *testing.T, cfg *config.Config) *workflowRuntime {
	t.Helper()
	runtime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	return runtime
}

func assertBearerAuth(t *testing.T, fake *remotetest.Server, appInvokes, agentCreates, workflowStarts, indexedDBGets int) {
	t.Helper()
	wantAuth := "Bearer " + fake.Token
	for _, rec := range fake.Recorder.AppInvokesSnapshot() {
		if rec.Authorization != wantAuth {
			t.Fatalf("app authorization = %q, want %q", rec.Authorization, wantAuth)
		}
	}
	if got := len(fake.Recorder.AppInvokesSnapshot()); got != appInvokes {
		t.Fatalf("remote app invokes = %d, want %d", got, appInvokes)
	}
	agentRecs := fake.Recorder.AgentCreatesSnapshot()
	if len(agentRecs) != agentCreates || (agentCreates > 0 && agentRecs[0].ProviderName != "managed") {
		t.Fatalf("agent creates = %#v, want managed", agentRecs)
	}
	wfRecs := fake.Recorder.WorkflowStartsSnapshot()
	if len(wfRecs) != workflowStarts || (workflowStarts > 0 && wfRecs[0].ProviderName != "default") {
		t.Fatalf("workflow starts = %#v, want default", wfRecs)
	}
	idbRecs := fake.Recorder.IndexedDBGetsSnapshot()
	if len(idbRecs) != indexedDBGets || (indexedDBGets > 0 && idbRecs[0].HostBinding != "archive") {
		t.Fatalf("indexeddb gets = %#v, want archive binding", idbRecs)
	}
}
