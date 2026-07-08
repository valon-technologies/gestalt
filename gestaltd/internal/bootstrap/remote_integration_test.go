package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/testutil/remotefake"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

const remoteIntegrationToken = "gst_api_remote_integration"

func TestRemoteIntegrationNothingLocalRoutesDeclaredProviders(t *testing.T) {
	t.Parallel()

	remoteSrv := startRemoteFake(t)
	cfg := remoteIntegrationBaseConfig(remoteSrv.BaseURL())
	cfg.Apps = map[string]*config.ProviderEntry{
		"linear":        remoteIntegrationAppEntry(t, "linear", "issues.list"),
		"valon-profile": remoteIntegrationAppEntry(t, "valon-profile", "profile.get"),
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"default": {Default: true},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"default": {Default: true},
	}

	deps := remoteIntegrationDeps(t, cfg, remoteSrv.BaseURL())
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil, coretesting.NewStubExternalCredentialProvider())
	pr := &principal.Principal{SubjectID: "user:test"}

	for _, tc := range []struct {
		app       string
		operation string
	}{
		{"linear", "issues.list"},
		{"valon-profile", "profile.get"},
	} {
		if _, err := broker.Invoke(context.Background(), pr, tc.app, "", tc.operation, nil); err != nil {
			t.Fatalf("Invoke(%s.%s): %v", tc.app, tc.operation, err)
		}
	}

	_, appCalls := remoteSrv.App.Snapshot()
	if len(appCalls) != 2 {
		t.Fatalf("remote app calls = %d, want 2", len(appCalls))
	}
	for _, call := range appCalls {
		if call.Auth != "Bearer "+remoteIntegrationToken {
			t.Fatalf("remote app auth = %q, want bearer token", call.Auth)
		}
	}

	_, agents, err := buildWorkflowsAndAgents(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildWorkflowsAndAgents: %v", err)
	}
	defer func() { _ = closeAgents(agents...) }()
	if len(agents) != 1 {
		t.Fatalf("agent providers = %d, want 1", len(agents))
	}
	if _, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		ProviderName: "default",
		Model:        "test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if lastAuth, calls := remoteSrv.Agent.Snapshot(); lastAuth != "Bearer "+remoteIntegrationToken || len(calls) != 1 {
		t.Fatalf("remote agent snapshot auth=%q calls=%d", lastAuth, len(calls))
	}
}

func TestRemoteIntegrationCICDLocalInvokesRemoteAppsAndAgent(t *testing.T) {
	t.Parallel()

	remoteSrv := startRemoteFake(t)
	cfg := remoteIntegrationBaseConfig(remoteSrv.BaseURL())
	cfg.Apps = map[string]*config.ProviderEntry{
		"ci-cd":         {DevActive: true},
		"linear":        remoteIntegrationAppEntry(t, "linear", "issues.list"),
		"valon-profile": remoteIntegrationAppEntry(t, "valon-profile", "profile.get"),
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"default": {Default: true},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "ci-cd") {
		t.Fatal("dev-active ci-cd should stay local")
	}

	localCICD := &coretesting.StubIntegration{
		N:        "ci-cd",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "ci-cd",
			Operations: []catalog.CatalogOperation{{
				ID:     "dispatch",
				Method: http.MethodPost,
			}},
		},
	}
	factories := NewFactoryRegistry()
	factories.Builtins = append(factories.Builtins, localCICD)

	buildCfg := *cfg
	buildCfg.Apps = map[string]*config.ProviderEntry{
		"linear":        cfg.Apps["linear"],
		"valon-profile": cfg.Apps["valon-profile"],
	}
	deps := remoteIntegrationDeps(t, cfg, remoteSrv.BaseURL())
	providers, _, err := buildProvidersStrict(context.Background(), &buildCfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil, coretesting.NewStubExternalCredentialProvider())
	pr := &principal.Principal{SubjectID: "user:test"}
	for _, tc := range []struct {
		app       string
		operation string
	}{
		{"linear", "issues.list"},
		{"valon-profile", "profile.get"},
	} {
		if _, err := broker.Invoke(context.Background(), pr, tc.app, "", tc.operation, nil); err != nil {
			t.Fatalf("Invoke(%s.%s): %v", tc.app, tc.operation, err)
		}
	}
	if _, appCalls := remoteSrv.App.Snapshot(); len(appCalls) != 2 {
		t.Fatalf("remote app calls = %d, want 2", len(appCalls))
	}

	_, agents, err := buildWorkflowsAndAgents(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildWorkflowsAndAgents: %v", err)
	}
	defer func() { _ = closeAgents(agents...) }()
	if _, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		ProviderName: "default",
		Model:        "test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, calls := remoteSrv.Agent.Snapshot(); len(calls) != 1 {
		t.Fatalf("remote agent calls = %d, want 1", len(calls))
	}
}

func TestRemoteIntegrationCICDAndProfileLocalKeepsProfileLocal(t *testing.T) {
	t.Parallel()

	remoteSrv := startRemoteFake(t)
	cfg := remoteIntegrationBaseConfig(remoteSrv.BaseURL())
	cfg.Apps = map[string]*config.ProviderEntry{
		"ci-cd":         {DevActive: true},
		"valon-profile": {DevActive: true},
		"linear":        remoteIntegrationAppEntry(t, "linear", "issues.list"),
	}
	cfg.Providers.Agent = map[string]*config.ProviderEntry{
		"default": {Default: true},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "valon-profile") {
		t.Fatal("dev-active valon-profile should stay local")
	}

	localProfile := &coretesting.StubIntegration{
		N:        "valon-profile",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "valon-profile",
			Operations: []catalog.CatalogOperation{{
				ID:     "profile.get",
				Method: http.MethodGet,
			}},
		},
		ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: http.StatusOK, Body: []byte(`{"local":true}`)}, nil
		},
	}
	localCICD := &coretesting.StubIntegration{
		N:        "ci-cd",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "ci-cd",
			Operations: []catalog.CatalogOperation{{
				ID:     "dispatch",
				Method: http.MethodPost,
			}},
		},
	}
	factories := NewFactoryRegistry()
	factories.Builtins = append(factories.Builtins, localCICD, localProfile)

	buildCfg := *cfg
	buildCfg.Apps = map[string]*config.ProviderEntry{
		"linear": cfg.Apps["linear"],
	}
	deps := remoteIntegrationDeps(t, cfg, remoteSrv.BaseURL())
	providers, _, err := buildProvidersStrict(context.Background(), &buildCfg, factories, deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil, coretesting.NewStubExternalCredentialProvider())
	pr := &principal.Principal{SubjectID: "user:test"}

	result, err := broker.Invoke(context.Background(), pr, "valon-profile", "", "profile.get", nil)
	if err != nil {
		t.Fatalf("Invoke(valon-profile): %v", err)
	}
	if string(result.Body) != `{"local":true}` {
		t.Fatalf("local profile body = %s, want local response", string(result.Body))
	}
	if _, appCalls := remoteSrv.App.Snapshot(); len(appCalls) != 0 {
		t.Fatalf("remote app calls = %d, want 0 for local profile", len(appCalls))
	}

	if _, err := broker.Invoke(context.Background(), pr, "linear", "", "issues.list", nil); err != nil {
		t.Fatalf("Invoke(linear): %v", err)
	}
	if _, appCalls := remoteSrv.App.Snapshot(); len(appCalls) != 1 {
		t.Fatalf("remote app calls = %d, want 1 for linear", len(appCalls))
	}

	_, agents, err := buildWorkflowsAndAgents(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildWorkflowsAndAgents: %v", err)
	}
	defer func() { _ = closeAgents(agents...) }()
	if _, err := agents[0].CreateSession(context.Background(), &proto.CreateAgentProviderSessionRequest{
		ProviderName: "default",
		Model:        "test",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

func TestRemoteIntegrationLocalStartupFailureDoesNotFallbackToRemote(t *testing.T) {
	t.Parallel()

	remoteSrv := startRemoteFake(t)
	cfg := remoteIntegrationBaseConfig(remoteSrv.BaseURL())
	cfg.Apps = map[string]*config.ProviderEntry{
		"ci-cd": {
			DevActive:            true,
			ResolvedManifest:     newExecutableManifest("CI/CD", "local app"),
			ResolvedManifestPath: filepath.Join(writeStaticCatalog(t, &catalog.Catalog{
				Name: "ci-cd",
				Operations: []catalog.CatalogOperation{{
					ID:     "dispatch",
					Method: http.MethodPost,
				}},
			}), "manifest.yaml"),
			Command: "/definitely/missing/gestalt-provider",
		},
	}

	deps := remoteIntegrationDeps(t, cfg, remoteSrv.BaseURL())
	_, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err == nil {
		t.Fatal("buildProvidersStrict = nil, want local startup failure")
	}
	if _, appCalls := remoteSrv.App.Snapshot(); len(appCalls) != 0 {
		t.Fatal("remote fallback should not occur after local startup failure")
	}
}

func TestRemoteIntegrationUndeclaredProviderRemainsNotFound(t *testing.T) {
	t.Parallel()

	remoteSrv := startRemoteFake(t)
	cfg := remoteIntegrationBaseConfig(remoteSrv.BaseURL())
	cfg.Apps = map[string]*config.ProviderEntry{
		"ci-cd": remoteIntegrationAppEntry(t, "ci-cd", "dispatch"),
	}

	deps := remoteIntegrationDeps(t, cfg, remoteSrv.BaseURL())
	providers, _, err := buildProvidersStrict(context.Background(), cfg, NewFactoryRegistry(), deps)
	if err != nil {
		t.Fatalf("buildProvidersStrict: %v", err)
	}
	defer func() { _ = CloseProviders(providers) }()

	broker := invocation.NewBroker(providers, nil, coretesting.NewStubExternalCredentialProvider())
	_, err = broker.Invoke(context.Background(), &principal.Principal{SubjectID: "user:test"}, "missing-app", "", "dispatch", nil)
	if err == nil {
		t.Fatal("Invoke = nil, want not found")
	}
	if !errors.Is(err, invocation.ErrProviderNotFound) {
		t.Fatalf("Invoke error = %v, want provider not found", err)
	}
}

func TestRemoteIntegrationDisabledWithoutRemoteConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"linear": remoteIntegrationAppEntry(t, "linear", "issues.list"),
		},
		Providers: config.ProvidersConfig{
			Agent: map[string]*config.ProviderEntry{
				"default": {Default: true},
			},
		},
	}
	plan, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	if plan.ShouldRouteRemote(RemoteProviderKindApp, "linear") {
		t.Fatal("linear should stay local when server.remote is empty")
	}
	if plan.ShouldRouteRemote(RemoteProviderKindAgent, "default") {
		t.Fatal("agent should stay local when server.remote is empty")
	}
}

func startRemoteFake(t *testing.T) *remotefake.Server {
	t.Helper()
	remoteSrv, err := remotefake.Start()
	if err != nil {
		t.Fatalf("remotefake.Start: %v", err)
	}
	t.Cleanup(func() { _ = remoteSrv.Close() })
	return remoteSrv
}

func remoteIntegrationBaseConfig(remoteURL string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Remote:        remoteURL,
			RemoteToken:   remoteIntegrationToken,
			EncryptionKey: "remote-integration-key",
			Providers: config.ServerProvidersConfig{
				IndexedDB: "test",
			},
		},
		Providers: config.ProvidersConfig{
			IndexedDB: map[string]*config.ProviderEntry{
				"test": {Source: config.ProviderSource{Builtin: "test-indexeddb"}},
			},
		},
	}
}

func remoteIntegrationAppEntry(t *testing.T, name, operation string) *config.ProviderEntry {
	t.Helper()
	root := writeStaticCatalog(t, &catalog.Catalog{
		Name: name,
		Operations: []catalog.CatalogOperation{{
			ID:     operation,
			Method: http.MethodGet,
		}},
	})
	return &config.ProviderEntry{
		ResolvedManifest:     newExecutableManifest(name, name+" provider"),
		ResolvedManifestPath: filepath.Join(root, "manifest.yaml"),
	}
}

func remoteIntegrationDeps(t *testing.T, cfg *config.Config, remoteURL string) Deps {
	t.Helper()
	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   remoteURL,
		Token: remoteIntegrationToken,
	})
	if err != nil {
		t.Fatalf("remote.NewClientSet: %v", err)
	}
	t.Cleanup(func() { _ = clientSet.Close() })

	placement, err := NewPlacementPlan(cfg)
	if err != nil {
		t.Fatalf("NewPlacementPlan: %v", err)
	}
	return Deps{
		Placement:     placement,
		RemoteClients: clientSet,
		Egress:        newEgressDeps(cfg),
	}
}
