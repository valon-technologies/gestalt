package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
			Auth: map[string]*config.ProviderEntry{
				"default": {
					Source: config.ProviderSource{Ref: "github.com/valon-technologies/gestalt-providers/auth/oidc", Version: "0.0.1-alpha.1"},
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
	f.Auth = func(yaml.Node, Deps) (core.AuthProvider, error) {
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
			ConnectionMode:       providermanifestv1.ConnectionModeIdentity,
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"status"},
			},
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
			UserID:      principal.IdentityPrincipal,
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-startup-token",
		}); err != nil {
			return nil, fmt.Errorf("store identity token: %w", err)
		}
		resp, err := invokeWorkflowHostDuringStartup(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			PluginName: "roadmap",
			Target: &proto.BoundWorkflowTarget{
				PluginName: "roadmap",
				Operation:  "status",
			},
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

func TestBootstrapPluginStartupWorkflowCallWaitsForWorkflowProvider(t *testing.T) {
	t.Parallel()

	bin, manifestRoot := buildModifiedExampleProviderBinary(t, func(source string) string {
		return strings.Replace(source,
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tif g, ok := config[\"greeting\"].(string); ok {\n\t\tp.greeting = g\n\t}\n\treturn nil\n}\n",
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tclient, err := gestalt.Workflow()\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer client.Close()\n\truns, err := client.ListRuns(context.Background())\n\tif err != nil {\n\t\treturn err\n\t}\n\tp.greeting = fmt.Sprintf(\"workflow-runs:%d\", len(runs))\n\treturn nil\n}\n",
			1,
		)
	})

	cfg := workflowStartupTestConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Command:              bin,
			ResolvedManifest:     newExecutableManifest("Roadmap", "Workflow startup client"),
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"status"},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := workflowStartupTestFactories()
	factories.Workflow = func(context.Context, string, yaml.Node, []providerhost.HostService, Deps) (coreworkflow.Provider, error) {
		time.Sleep(300 * time.Millisecond)
		return startupTestWorkflowProvider{
			listRuns: func(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
				return []*coreworkflow.Run{{
					ID: "run-1",
					Target: coreworkflow.Target{
						PluginName: "roadmap",
						Operation:  "status",
					},
				}}, nil
			},
		}, nil
	}

	result, err := Bootstrap(context.Background(), cfg, factories)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer func() { _ = result.Close(context.Background()) }()
	<-result.ProvidersReady

	prov, err := result.Providers.Get("roadmap")
	if err != nil {
		t.Fatalf("providers.Get(roadmap): %v", err)
	}
	resp, err := prov.Execute(context.Background(), "status", nil, "")
	if err != nil {
		t.Fatalf("Execute(status): %v", err)
	}
	var status struct {
		Greeting string `json:"greeting"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &status); err != nil {
		t.Fatalf("json.Unmarshal(status): %v", err)
	}
	if status.Greeting != "workflow-runs:1" {
		t.Fatalf("status.greeting = %q, want %q", status.Greeting, "workflow-runs:1")
	}
}

func TestValidatePluginStartupWorkflowCallWaitsForWorkflowProvider(t *testing.T) {
	t.Parallel()

	bin, manifestRoot := buildModifiedExampleProviderBinary(t, func(source string) string {
		return strings.Replace(source,
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tif g, ok := config[\"greeting\"].(string); ok {\n\t\tp.greeting = g\n\t}\n\treturn nil\n}\n",
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tclient, err := gestalt.Workflow()\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer client.Close()\n\truns, err := client.ListRuns(context.Background())\n\tif err != nil {\n\t\treturn err\n\t}\n\tp.greeting = fmt.Sprintf(\"workflow-runs:%d\", len(runs))\n\treturn nil\n}\n",
			1,
		)
	})

	cfg := workflowStartupTestConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Command:              bin,
			ResolvedManifest:     newExecutableManifest("Roadmap", "Workflow startup client"),
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"status"},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := workflowStartupTestFactories()
	factories.Workflow = func(context.Context, string, yaml.Node, []providerhost.HostService, Deps) (coreworkflow.Provider, error) {
		time.Sleep(300 * time.Millisecond)
		return startupTestWorkflowProvider{
			listRuns: func(context.Context, coreworkflow.ListRunsRequest) ([]*coreworkflow.Run, error) {
				return []*coreworkflow.Run{{
					ID: "run-1",
					Target: coreworkflow.Target{
						PluginName: "roadmap",
						Operation:  "status",
					},
				}}, nil
			},
		}, nil
	}

	if _, err := Validate(context.Background(), cfg, factories); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestBootstrapFailsPendingWorkflowStartupClientsOnAuthorizationErrors(t *testing.T) {
	bin, manifestRoot := buildModifiedExampleProviderBinary(t, func(source string) string {
		return strings.Replace(source,
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tif g, ok := config[\"greeting\"].(string); ok {\n\t\tp.greeting = g\n\t}\n\treturn nil\n}\n",
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tclient, err := gestalt.Workflow()\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer client.Close()\n\t_, err = client.ListRuns(context.Background())\n\treturn err\n}\n",
			1,
		)
	})

	cfg := workflowStartupTestConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Command:              bin,
			ResolvedManifest:     newExecutableManifest("Roadmap", "Workflow startup client"),
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"status"},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}
	cfg.Authorization = config.AuthorizationConfig{
		Workloads: map[string]config.WorkloadDef{
			"workflow.roadmap": {
				Token: "gst_wld_conflict",
			},
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := Bootstrap(context.Background(), cfg, workflowStartupTestFactories())
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `conflicts with configured workload`) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Bootstrap hung while a plugin waited on a workflow startup proxy")
	}
}

func TestBootstrapFailsWorkflowStartupDependencyCycles(t *testing.T) {
	bin, manifestRoot := buildModifiedExampleProviderBinary(t, func(source string) string {
		return strings.Replace(source,
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tif g, ok := config[\"greeting\"].(string); ok {\n\t\tp.greeting = g\n\t}\n\treturn nil\n}\n",
			"func (p *Provider) Configure(_ context.Context, name string, config map[string]any) error {\n\tp.startedName = name\n\tclient, err := gestalt.Workflow()\n\tif err != nil {\n\t\treturn err\n\t}\n\tdefer client.Close()\n\t_, err = client.ListRuns(context.Background())\n\treturn err\n}\n",
			1,
		)
	})

	cfg := workflowStartupTestConfig()
	cfg.Plugins = map[string]*config.ProviderEntry{
		"roadmap": {
			Command:              bin,
			ResolvedManifest:     newExecutableManifest("Roadmap", "Workflow startup client"),
			ResolvedManifestPath: filepath.Join(manifestRoot, "manifest.yaml"),
			ConnectionMode:       providermanifestv1.ConnectionModeIdentity,
			Workflow: &config.PluginWorkflowConfig{
				Provider:   "temporal",
				Operations: []string{"status"},
			},
		},
	}
	cfg.Providers.Workflow = map[string]*config.ProviderEntry{
		"temporal": {Source: config.ProviderSource{Path: "stub"}},
	}

	factories := workflowStartupTestFactories()
	factories.Workflow = func(_ context.Context, name string, _ yaml.Node, hostServices []providerhost.HostService, deps Deps) (coreworkflow.Provider, error) {
		if err := deps.Services.Tokens.StoreToken(context.Background(), &core.IntegrationToken{
			UserID:      principal.IdentityPrincipal,
			Integration: "roadmap",
			Connection:  config.PluginConnectionName,
			Instance:    "default",
			AccessToken: "workflow-cycle-token",
		}); err != nil {
			return nil, fmt.Errorf("store identity token: %w", err)
		}
		_, err := invokeWorkflowHostDuringStartup(t, hostServices, &proto.InvokeWorkflowOperationRequest{
			PluginName: "roadmap",
			Target: &proto.BoundWorkflowTarget{
				PluginName: "roadmap",
				Operation:  "status",
			},
		})
		if err == nil {
			return nil, fmt.Errorf("expected startup dependency cycle")
		}
		return nil, err
	}

	done := make(chan error, 1)
	go func() {
		_, err := Bootstrap(context.Background(), cfg, factories)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), `workflow startup dependency cycle`) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Bootstrap hung on a workflow/plugin startup dependency cycle")
	}
}
