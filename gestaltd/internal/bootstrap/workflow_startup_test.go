package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"gopkg.in/yaml.v3"
)

type startupTestWorkflowProvider struct {
	*noopWorkflowProvider
}

type startableStartupTestWorkflowProvider struct {
	startupTestWorkflowProvider
	started int
}

func (p *startableStartupTestWorkflowProvider) Start(context.Context) error {
	p.started++
	return nil
}

type noopTelemetryProvider struct{}

func TestWorkflowRuntimeResolvePendingProviderWaitsForContext(t *testing.T) {
	t.Parallel()

	runtime, err := newWorkflowRuntime(&config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {},
			},
		},
	})
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	runtime.InitProviderPlaceholders(map[string]*config.ProviderEntry{
		"temporal": {},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := runtime.ResolveProvider(ctx, "temporal"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ResolveProvider error = %v, want context deadline", err)
	}
}

func TestWorkflowRuntimeSkipsConfiguredProviderPublishAfterFailure(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Providers: config.ProvidersConfig{
			Workflow: map[string]*config.ProviderEntry{
				"temporal": {},
			},
		},
	}
	runtime, err := newWorkflowRuntime(cfg)
	if err != nil {
		t.Fatalf("newWorkflowRuntime: %v", err)
	}
	runtime.InitProviderPlaceholders(cfg.Providers.Workflow)
	runtime.FailPendingProviders(errors.New("boom"))
	runtime.PublishProvider("temporal", startupTestWorkflowProvider{})
	if _, _, err := runtime.ResolveProvider(context.Background(), "temporal"); err == nil {
		t.Fatal("configured provider was published after startup failure")
	}
}

func TestBuildProviderHostServicesDoesNotRequireConfiguredEncryptionKey(t *testing.T) {
	t.Parallel()

	hostServices, invTokens, err := buildProviderHostServices("metadata", Deps{
		Services: &coredata.Services{
			ExternalCredentials: coretesting.NewStubExternalCredentialProvider(),
		},
		AuthorizationProvider: &hostedHTTPAuthorizationProvider{},
	})
	if err != nil {
		t.Fatalf("buildProviderHostServices: %v", err)
	}
	if invTokens == nil {
		t.Fatal("invocation token manager is nil")
	}

	names := hostServiceNames(hostServices)
	for _, want := range []string{"app", "workflow_provider", "agent_provider", "external_credentials", "authorization"} {
		if !hasHostServiceName(names, want) {
			t.Fatalf("provider host services missing %q: %v", want, names)
		}
	}
}

func TestBuildWorkflowRegistersIndexedDBPublicRelay(t *testing.T) {
	t.Parallel()

	factories := NewFactoryRegistry()
	factories.Workflow = func(context.Context, string, yaml.Node, []runtimehost.HostService, Deps) (coreworkflow.Provider, error) {
		return startupTestWorkflowProvider{}, nil
	}
	deps := Deps{
		BaseURL:               "https://gestalt.example.test",
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		SelectedIndexedDBName: "main",
		IndexedDBs: map[string]indexeddb.IndexedDB{
			"main": &coretesting.StubIndexedDB{},
		},
		IndexedDBDefs: map[string]*config.ProviderEntry{
			"main": {
				Source: config.NewMetadataSource("https://example.invalid/indexeddb/relationaldb/v0.0.1-alpha.2/provider-release.yaml"),
				Config: mustNode(t, map[string]any{"bucket": "workflow-state"}),
			},
		},
		IndexedDBFactory: func(yaml.Node) (indexeddb.IndexedDB, error) {
			return &coretesting.StubIndexedDB{}, nil
		},
		PublicHostServices: runtimehost.NewPublicHostServiceRegistry(),
	}
	provider, err := buildWorkflow(context.Background(), "local", &config.ProviderEntry{
		Config:    mustNode(t, map[string]any{"command": "/bin/workflow-provider"}),
		IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
	}, factories, deps)
	if err != nil {
		t.Fatalf("buildWorkflow: %v", err)
	}

	assertPublicHostServicesVerified(t, deps.PublicHostServices, "indexeddb")
	if err := provider.Close(); err != nil {
		t.Fatalf("provider.Close: %v", err)
	}
	if services := deps.PublicHostServices.Snapshot(); len(services) != 0 {
		t.Fatalf("public host services after provider close = %#v, want none", services)
	}
}

func TestBuildWorkflowPassesAuthorizationHostService(t *testing.T) {
	t.Parallel()

	var hostServiceNames []string
	factories := NewFactoryRegistry()
	factories.Workflow = func(_ context.Context, _ string, _ yaml.Node, hostServices []runtimehost.HostService, _ Deps) (coreworkflow.Provider, error) {
		for _, hostService := range hostServices {
			hostServiceNames = append(hostServiceNames, hostService.Name)
		}
		return startupTestWorkflowProvider{}, nil
	}
	provider, err := buildWorkflow(context.Background(), "local", &config.ProviderEntry{
		Config: mustNode(t, map[string]any{"command": "/bin/workflow-provider"}),
	}, factories, Deps{
		AuthorizationProvider: &hostedHTTPAuthorizationProvider{},
	})
	if err != nil {
		t.Fatalf("buildWorkflow: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("provider.Close: %v", err)
	}

	if !hasHostServiceName(hostServiceNames, "authorization") {
		t.Fatalf("workflow provider host services = %v, want authorization", hostServiceNames)
	}
}

func hasHostServiceName(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hostServiceNames(hostServices []runtimehost.HostService) []string {
	names := make([]string, 0, len(hostServices))
	for _, hostService := range hostServices {
		names = append(names, hostService.Name)
	}
	return names
}

func (noopTelemetryProvider) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}
func (noopTelemetryProvider) TracerProvider() trace.TracerProvider {
	return tracenoop.NewTracerProvider()
}
func (noopTelemetryProvider) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}
func (noopTelemetryProvider) PrometheusHandler() http.Handler { return http.NotFoundHandler() }
func (noopTelemetryProvider) Shutdown(context.Context) error  { return nil }

func TestResultStartWorkflowProvidersIsSeparateFromAuthorizerStart(t *testing.T) {
	t.Parallel()

	provider := &startableStartupTestWorkflowProvider{}
	result := &Result{
		ExtraWorkflows: []coreworkflow.Provider{provider},
	}

	if err := result.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if provider.started != 0 {
		t.Fatalf("workflow provider started during Result.Start = %d, want 0", provider.started)
	}
	if err := result.StartWorkflowProviders(context.Background()); err != nil {
		t.Fatalf("StartWorkflowProviders: %v", err)
	}
	if provider.started != 1 {
		t.Fatalf("workflow provider started = %d, want 1", provider.started)
	}
}

func TestWorkflowCleanupWrappersForwardStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider coreworkflow.Provider
	}{
		{
			name: "cleanup",
			provider: &workflowProviderWithCleanup{
				Provider: &startableStartupTestWorkflowProvider{},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := &Result{ExtraWorkflows: []coreworkflow.Provider{tc.provider}}
			if err := result.StartWorkflowProviders(context.Background()); err != nil {
				t.Fatalf("StartWorkflowProviders: %v", err)
			}
			var inner *startableStartupTestWorkflowProvider
			switch provider := tc.provider.(type) {
			case *workflowProviderWithCleanup:
				inner = provider.Provider.(*startableStartupTestWorkflowProvider)
			default:
				t.Fatalf("unexpected provider type %T", provider)
			}
			if inner.started != 1 {
				t.Fatalf("inner started = %d, want 1", inner.started)
			}
		})
	}
}
