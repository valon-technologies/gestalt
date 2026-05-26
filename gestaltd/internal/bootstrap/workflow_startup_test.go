package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"gopkg.in/yaml.v3"
)

type startupTestWorkflowProvider struct {
	listRuns func(context.Context, coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error)
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

func (p startupTestWorkflowProvider) CreateDefinition(context.Context, coreworkflow.CreateDefinitionRequest) (*coreworkflow.Definition, error) {
	return &coreworkflow.Definition{}, nil
}

func (p startupTestWorkflowProvider) GetDefinition(context.Context, coreworkflow.GetDefinitionRequest) (*coreworkflow.Definition, error) {
	return &coreworkflow.Definition{}, nil
}

func (p startupTestWorkflowProvider) UpdateDefinition(context.Context, coreworkflow.UpdateDefinitionRequest) (*coreworkflow.Definition, error) {
	return &coreworkflow.Definition{}, nil
}

func (p startupTestWorkflowProvider) DeleteDefinition(context.Context, coreworkflow.DeleteDefinitionRequest) error {
	return nil
}

func (p startupTestWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p startupTestWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p startupTestWorkflowProvider) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) (*coreworkflow.ListRunsResponse, error) {
	if p.listRuns != nil {
		return p.listRuns(ctx, req)
	}
	return &coreworkflow.ListRunsResponse{}, nil
}

func (p startupTestWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p startupTestWorkflowProvider) SignalRun(context.Context, coreworkflow.SignalRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}

func (p startupTestWorkflowProvider) SignalOrStartRun(context.Context, coreworkflow.SignalOrStartRunRequest) (*coreworkflow.SignalRunResponse, error) {
	return &coreworkflow.SignalRunResponse{Run: &coreworkflow.Run{}}, nil
}

func (p startupTestWorkflowProvider) UpsertSchedule(context.Context, coreworkflow.UpsertScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}

func (p startupTestWorkflowProvider) GetSchedule(context.Context, coreworkflow.GetScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}

func (p startupTestWorkflowProvider) ListSchedules(context.Context, coreworkflow.ListSchedulesRequest) ([]*coreworkflow.Schedule, error) {
	return nil, nil
}

func (p startupTestWorkflowProvider) DeleteSchedule(context.Context, coreworkflow.DeleteScheduleRequest) error {
	return nil
}

func (p startupTestWorkflowProvider) PauseSchedule(context.Context, coreworkflow.PauseScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}

func (p startupTestWorkflowProvider) ResumeSchedule(context.Context, coreworkflow.ResumeScheduleRequest) (*coreworkflow.Schedule, error) {
	return &coreworkflow.Schedule{}, nil
}

func (p startupTestWorkflowProvider) UpsertEventTrigger(context.Context, coreworkflow.UpsertEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}

func (p startupTestWorkflowProvider) GetEventTrigger(context.Context, coreworkflow.GetEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}

func (p startupTestWorkflowProvider) ListEventTriggers(context.Context, coreworkflow.ListEventTriggersRequest) ([]*coreworkflow.EventTrigger, error) {
	return nil, nil
}

func (p startupTestWorkflowProvider) DeleteEventTrigger(context.Context, coreworkflow.DeleteEventTriggerRequest) error {
	return nil
}

func (p startupTestWorkflowProvider) PauseEventTrigger(context.Context, coreworkflow.PauseEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}

func (p startupTestWorkflowProvider) ResumeEventTrigger(context.Context, coreworkflow.ResumeEventTriggerRequest) (*coreworkflow.EventTrigger, error) {
	return &coreworkflow.EventTrigger{}, nil
}

func (p startupTestWorkflowProvider) PublishEvent(_ context.Context, req coreworkflow.PublishEventRequest) (*coreworkflow.Event, error) {
	return &req.Event, nil
}

func (p startupTestWorkflowProvider) Ping(context.Context) error { return nil }
func (p startupTestWorkflowProvider) Close() error               { return nil }

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
