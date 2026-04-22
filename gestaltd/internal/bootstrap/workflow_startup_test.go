package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gopkg.in/yaml.v3"
)

type startupTestWorkflowProvider struct {
	listRuns func(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error)
}

type noopTelemetryProvider struct{}

func (p startupTestWorkflowProvider) StartRun(context.Context, coreworkflow.StartRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p startupTestWorkflowProvider) GetRun(context.Context, coreworkflow.GetRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
}

func (p startupTestWorkflowProvider) ListRuns(ctx context.Context, req coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
	if p.listRuns != nil {
		return p.listRuns(ctx, req)
	}
	return nil, nil
}

func (p startupTestWorkflowProvider) CancelRun(context.Context, coreworkflow.CancelRunRequest) (*coreworkflow.Run, error) {
	return &coreworkflow.Run{}, nil
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

func (p startupTestWorkflowProvider) PublishEvent(context.Context, coreworkflow.PublishEventRequest) error {
	return nil
}

func (p startupTestWorkflowProvider) Ping(context.Context) error { return nil }
func (p startupTestWorkflowProvider) Close() error               { return nil }

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

func workflowStartupTestConfig() *config.Config {
	return &config.Config{
		Plugins: map[string]*config.ProviderEntry{},
		Providers: config.ProvidersConfig{
			Authentication: map[string]*config.ProviderEntry{
				"default": {
					Source: config.NewMetadataSource("https://example.invalid/github-com-valon-technologies-gestalt-providers-auth-oidc/v0.0.1-alpha.1/provider-release.yaml"),
					Config: yaml.Node{Kind: yaml.MappingNode},
				},
			},
			Secrets: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "test-secrets"}},
			},
			Telemetry: map[string]*config.ProviderEntry{
				"default": {Source: config.ProviderSource{Builtin: "test-telemetry"}},
			},
			IndexedDB: map[string]*config.ProviderEntry{
				"test": {Source: config.ProviderSource{Path: "stub"}},
			},
		},
		Server: config.ServerConfig{
			Public:        config.ListenerConfig{Port: 8080},
			EncryptionKey: "test-key",
			Providers:     config.ServerProvidersConfig{IndexedDB: "test"},
		},
	}
}

func workflowStartupTestFactories() *FactoryRegistry {
	f := NewFactoryRegistry()
	f.Auth = func(yaml.Node, Deps) (core.AuthenticationProvider, error) {
		return &coretesting.StubAuthProvider{N: "test-auth"}, nil
	}
	f.IndexedDB = func(yaml.Node) (indexeddb.IndexedDB, error) {
		return &coretesting.StubIndexedDB{}, nil
	}
	f.Secrets["test-secrets"] = func(yaml.Node) (core.SecretManager, error) {
		return &coretesting.StubSecretManager{}, nil
	}
	f.Telemetry["test-telemetry"] = func(yaml.Node) (core.TelemetryProvider, error) {
		return noopTelemetryProvider{}, nil
	}
	return f
}

func buildModifiedExampleProviderBinary(t *testing.T, mutate func(string) string) (string, string) {
	t.Helper()

	root := t.TempDir()
	testutil.CopyExampleProviderPlugin(t, root)
	providerPath := filepath.Join(root, "provider.go")
	source, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider.go: %v", err)
	}
	updated := mutate(string(source))
	if err := os.WriteFile(providerPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write provider.go: %v", err)
	}

	bin := filepath.Join(root, filepath.Base(root))
	if err := buildBootstrapTestBinary(root, "", bin); err != nil {
		t.Fatalf("build custom provider binary: %v", err)
	}
	return bin, root
}

func invokeWorkflowHostDuringStartup(t *testing.T, hostServices []providerhost.HostService, req *proto.InvokeWorkflowOperationRequest) (*proto.InvokeWorkflowOperationResponse, error) {
	t.Helper()

	if len(hostServices) != 1 {
		t.Fatalf("workflow host services = %d, want 1", len(hostServices))
	}
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	hostServices[0].Register(srv)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return proto.NewWorkflowHostClient(conn).InvokeOperation(context.Background(), req)
}

func storeStartupExecutionRef(t *testing.T, deps Deps, providerName string, target coreworkflow.Target) string {
	t.Helper()
	ref, err := deps.Services.WorkflowExecutionRefs.Put(context.Background(), &coreworkflow.ExecutionReference{
		ID:           fmt.Sprintf("startup:%s:%s:%s", strings.ReplaceAll(t.Name(), "/", "_"), providerName, target.Operation),
		ProviderName: providerName,
		Target:       target,
		SubjectID:    "system:config",
		Permissions:  workflowExecutionRefPermissionsForTarget(target),
	})
	if err != nil {
		t.Fatalf("store workflow execution ref: %v", err)
	}
	return ref.ID
}

func TestBootstrapWorkflowStartupCallbackWaitsForDelayedPluginProvider(t *testing.T) {
	t.Parallel()

	bin, manifestRoot := buildModifiedExampleProviderBinary(t, func(source string) string {
		source = strings.Replace(source, "\"net/http\"\n", "\"net/http\"\n\t\"time\"\n", 1)
		return strings.Replace(source,
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n",
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\ttime.Sleep(300 * time.Millisecond)\n",
			1,
		)
	})

	cfg := workflowStartupTestConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Command:              bin,
			ResolvedManifest:     newExecutableManifest("Roadmap", "Delayed startup provider"),
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			ConnectionMode:       providermanifestv1.ConnectionModeUser,
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := workflowStartupTestFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []providerhost.HostService, deps Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
			SubjectID:   "system:config",
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-startup-token",
		}); err != nil {
			return nil, fmt.Errorf("store startup token: %w", err)
		}
		executionRef := storeStartupExecutionRef(t, deps, name, coreworkflow.Target{
			PluginName: "roadmap",
			Operation:  "status",
		})
		resp, err := invokeWorkflowHostDuringStartup(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			Target: &proto.BoundWorkflowTarget{
				PluginName: "roadmap",
				Operation:  "status",
			},
			ExecutionRef: executionRef,
		})
		if err != nil {
			return nil, fmt.Errorf("startup callback: %w", err)
		}
		if resp.GetStatus() != http.StatusOK {
			return nil, fmt.Errorf("startup callback status = %d, want %d", resp.GetStatus(), http.StatusOK)
		}
		if !strings.Contains(resp.GetBody(), `"name":"roadmap"`) {
			return nil, fmt.Errorf("startup callback body = %q", resp.GetBody())
		}
		return startupTestWorkflowProvider{}, nil
	}

	result, err := Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady
}

func TestManagedWorkflowStartupCallbackRequiresExecutionRef(t *testing.T) {
	t.Parallel()

	bin, manifestRoot := buildModifiedExampleProviderBinary(t, func(source string) string { return source })

	cfg := workflowStartupTestConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Command:              bin,
			ResolvedManifest:     newExecutableManifest("Roadmap", "Startup callback provider"),
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			ConnectionMode:       providermanifestv1.ConnectionModeUser,
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := workflowStartupTestFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []providerhost.HostService, deps Deps) (coreworkflow.Provider, error) {
		if name != "temporal" {
			return nil, fmt.Errorf("workflow name = %q, want %q", name, "temporal")
		}
		if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
			SubjectID:   "system:config",
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-startup-token",
		}); err != nil {
			return nil, fmt.Errorf("store startup token: %w", err)
		}
		_, err := invokeWorkflowHostDuringStartup(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			Target: &proto.BoundWorkflowTarget{
				PluginName: "roadmap",
				Operation:  "status",
			},
		})
		if err == nil {
			return nil, fmt.Errorf("startup callback unexpectedly succeeded without execution_ref")
		}
		if got, want := status.Code(err), codes.InvalidArgument; got != want {
			return nil, fmt.Errorf("startup callback code = %v, want %v (err=%v)", got, want, err)
		}
		return startupTestWorkflowProvider{}, nil
	}

	result, err := Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady
}
