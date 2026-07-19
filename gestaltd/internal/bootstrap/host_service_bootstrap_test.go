package bootstrap

import (
	"context"
	"net"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func eagerHostServiceTestDeps(registry *runtimehost.PublicHostServiceRegistry) Deps {
	return Deps{
		BaseURL:               "https://gestalt.example.test",
		EncryptionKey:         []byte("0123456789abcdef0123456789abcdef"),
		SelectedIndexedDBName: "main",
		IndexedDBs: map[string]indexeddb.IndexedDB{
			"main": &coretesting.StubIndexedDB{},
		},
		PublicHostServices: registry,
		Runtime:            newCapturingRuntime(),
	}
}

func appIndexedDBRegistered(registry *runtimehost.PublicHostServiceRegistry, appName string) bool {
	for _, service := range registry.Snapshot() {
		if service.AppName == appName && service.Service.Name == "indexeddb" {
			return true
		}
	}
	return false
}

func TestRegisterConfiguredAppPublicHostServicesRegistersDefaultRuntimeApp(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"gIssues": {
				IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
			},
		},
	}

	cleanup := registerConfiguredAppPublicHostServices(cfg, deps)

	assertPublicHostServicesVerified(t, registry, "indexeddb")
	if !appIndexedDBRegistered(registry, "gIssues") {
		t.Fatalf("registry = %#v, want indexeddb host service for gIssues before activation", registry.Snapshot())
	}

	cleanup()
	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("after cleanup registry = %#v, want none", services)
	}
}

func TestRegisterConfiguredAppPublicHostServicesRegistersDevActiveWithoutSupervisor(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"gIssues": {
				DevActive: true,
				IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
			},
		},
	}

	registerConfiguredAppPublicHostServices(cfg, deps)

	if !appIndexedDBRegistered(registry, "gIssues") {
		t.Fatalf("registry = %#v, want indexeddb host service registered without a DevSupervisor", registry.Snapshot())
	}
}

func TestRegisterConfiguredAppPublicHostServicesNoopWithoutRelay(t *testing.T) {
	t.Parallel()

	registry := runtimehost.NewPublicHostServiceRegistry()
	deps := eagerHostServiceTestDeps(registry)
	deps.EncryptionKey = nil
	cfg := &config.Config{
		Apps: map[string]*config.ProviderEntry{
			"gIssues": {
				IndexedDB: &config.IndexedDBBindingConfig{Provider: "main"},
			},
		},
	}

	registerConfiguredAppPublicHostServices(cfg, deps)

	if services := registry.Snapshot(); len(services) != 0 {
		t.Fatalf("registry = %#v, want no registration when the public relay is unavailable", services)
	}
}

// checkAccessRecordingAuthorizationProvider is a sentinel authorization
// provider that records CheckAccess calls so tests can assert which provider
// instance a host service consulted. It embeds core.AuthorizationProvider for
// forward compatibility with the full interface surface.
type checkAccessRecordingAuthorizationProvider struct {
	core.AuthorizationProvider
	name          string
	checkAccesses []*proto.CheckAccessRequest
}

func (p *checkAccessRecordingAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.checkAccesses = append(p.checkAccesses, req)
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (p *checkAccessRecordingAuthorizationProvider) CheckAccessCount() int {
	return len(p.checkAccesses)
}

func (p *checkAccessRecordingAuthorizationProvider) Close() error { return nil }

func (p *checkAccessRecordingAuthorizationProvider) Ping(context.Context) error { return nil }

// withHostServiceClient registers a host service on an in-memory gRPC server
// and invokes fn with a fresh client. The server is torn down when fn returns.
func withHostServiceClient[T any](t *testing.T, hostService runtimehost.HostService, newClient func(grpc.ClientConnInterface) T, fn func(T)) {
	t.Helper()
	if hostService.Register == nil {
		t.Fatalf("host service %q has no Register func", hostService.Name)
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	hostService.Register(srv)
	go func() { _ = srv.Serve(lis) }()
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
	defer func() { _ = conn.Close() }()

	fn(newClient(conn))
}

// TestBuildProviderHostServicesWiresAuthorizationSplit asserts the provider
// gateway enforcement split: the plugin-facing authorization host service
// receives the Guarded (gateway-guarded) authorization provider, while the
// plugin-facing workflow ProviderServer receives the Raw (direct) provider so
// gestaltd's own requireWorkflowAccess/requireServiceAccountManagement checks
// are never meta-checked by the gateway.
func TestBuildProviderHostServicesWiresAuthorizationSplit(t *testing.T) {
	t.Parallel()

	guarded := &checkAccessRecordingAuthorizationProvider{name: "guarded"}
	raw := &checkAccessRecordingAuthorizationProvider{name: "raw"}

	hostServices, err := buildProviderHostServices("testapp", Deps{
		Authorization:         guarded,
		AuthorizationInternal: raw,
		Services: &coredata.Services{
			ExternalCredentials: coretesting.NewStubExternalCredentialProvider(),
		},
	})
	if err != nil {
		t.Fatalf("buildProviderHostServices: %v", err)
	}

	names := hostServiceNames(hostServices)
	if !hasHostServiceName(names, "authorization") {
		t.Fatalf("provider host services missing %q: %v", "authorization", names)
	}
	if !hasHostServiceName(names, "workflow") {
		t.Fatalf("provider host services missing %q: %v", "workflow", names)
	}
	var authorizationHostService, workflowHostService runtimehost.HostService
	for _, hostService := range hostServices {
		switch hostService.Name {
		case "authorization":
			authorizationHostService = hostService
		case "workflow":
			workflowHostService = hostService
		}
	}

	ctx := context.Background()

	// The authorization host service must consult the Guarded provider.
	withHostServiceClient(t, authorizationHostService, proto.NewAuthorizationClient, func(client proto.AuthorizationClient) {
		if _, err := client.CheckAccess(ctx, invocation.SubjectAccessRequest(
			"user:test", "check", &proto.Resource{Type: "provider", Id: "indexeddb"},
		)); err != nil {
			t.Fatalf("authorization CheckAccess: %v", err)
		}
	})
	if guarded.CheckAccessCount() != 1 {
		t.Fatalf("guarded authorization provider CheckAccess count = %d, want 1; the authorization host service must use the Guarded (gateway-guarded) provider", guarded.CheckAccessCount())
	}
	if raw.CheckAccessCount() != 0 {
		t.Fatalf("raw authorization provider CheckAccess count = %d, want 0; the authorization host service must not consult the Raw provider", raw.CheckAccessCount())
	}

	// The workflow host service must consult the Raw provider for its own
	// enforcement checks. ListRuns runs requireWorkflowAccess (which calls
	// CheckAccess on the wired provider) before reaching the (unavailable)
	// workflow manager, so the RPC may fail afterwards; only the recording
	// matters here.
	withHostServiceClient(t, workflowHostService, proto.NewWorkflowClient, func(client proto.WorkflowClient) {
		_, _ = client.ListRuns(ctx, &proto.ListWorkflowProviderRunsRequest{
			Provider: "testapp",
			Context: &proto.RequestContext{
				Caller: &proto.ProviderContext{
					Kind: string(invocation.ProviderKindApp),
					Name: "testapp",
				},
				Subject: &proto.SubjectContext{Id: "user:test"},
			},
		})
	})
	if raw.CheckAccessCount() != 1 {
		t.Fatalf("raw authorization provider CheckAccess count = %d, want 1; the workflow host service must use the Raw (direct) provider for its own enforcement checks", raw.CheckAccessCount())
	}
	if guarded.CheckAccessCount() != 1 {
		t.Fatalf("guarded authorization provider CheckAccess count = %d, want 1; the workflow host service must not meta-check via the Guarded provider", guarded.CheckAccessCount())
	}
}
