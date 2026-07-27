package remotepublish_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	coredata "github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/tunnel"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/remotemanagement"
	"github.com/valon-technologies/gestalt/server/services/remotepublish"
)

// upstreamHarness is an in-process upstream (frps + RemoteManagement + gRPC
// server) for publisher tests.
type upstreamHarness struct {
	rmService *remotemanagement.Service
	remoteCfg *config.RemoteConfig
	providers *registry.ProviderMap[core.Provider]
	frps      *tunnel.Server
	conn      *publicrpc.InProcessConn
	httpLn    net.Listener
	httpSrv   *http.Server
}

func newUpstreamHarness(t *testing.T) (*upstreamHarness, context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	frps, err := tunnel.StartServer(ctx)
	if err != nil {
		cancel()
		t.Fatalf("start frps: %v", err)
	}

	// Mount the frps WebSocket handler on a test HTTP server so frpc can
	// connect via ws://127.0.0.1:<port>/~!frp.
	frpsLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		frps.Close()
		t.Fatalf("frps http listen: %v", err)
	}
	frpsHTTP := &http.Server{Handler: frps.HTTPHandler()}
	go func() { _ = frpsHTTP.Serve(frpsLn) }()

	upstreamID, err := tunnel.NewIdentity()
	if err != nil {
		cancel()
		frps.Close()
		t.Fatalf("upstream identity: %v", err)
	}

	services, err := coredata.New(&coretesting.StubIndexedDB{})
	if err != nil {
		cancel()
		frps.Close()
		t.Fatalf("coredata.New: %v", err)
	}
	services.RemoteRegistrations.SetClock(func() time.Time { return time.Now().UTC() })

	validator := remotepublish.NewEndpointValidator(remotepublish.EndpointValidatorConfig{
		ConnectAddr:    frps.ConnectAddr(),
		ClientIdentity: upstreamID,
	})

	rmService, err := remotemanagement.New(
		services.RemoteRegistrations,
		nil,
		services.Users,
		validator,
		remotemanagement.Config{
			ServerIdentity: &proto.ServerIdentity{ClientSpkiSha256: upstreamID.SPKISHA256},
			Tunnel: &proto.TunnelBootstrap{
				FrpsAddress:   "ws://" + frpsLn.Addr().String(),
				LeaseDuration: durationpb.New(30 * time.Second),
			},
			LeaseDuration: 30 * time.Second,
		},
	)
	if err != nil {
		cancel()
		frps.Close()
		t.Fatalf("remotemanagement.New: %v", err)
	}

	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = principal.WithPrincipal(ctx, &principal.Principal{SubjectID: "user:alice@example.com"})
		return handler(ctx, req)
	}))
	publicrpc.RegisterPublicServers(grpcSrv, publicrpc.Servers{
		RemoteManagement: rmService,
	})
	conn, err := publicrpc.NewInProcessConn(grpcSrv)
	if err != nil {
		cancel()
		frps.Close()
		t.Fatalf("in-process conn: %v", err)
	}
	mux := runtime.NewServeMux()
	if err := publicrpc.RegisterRESTGateway(context.Background(), mux, conn.ClientConn(), publicrpc.Servers{
		RemoteManagement: rmService,
	}); err != nil {
		conn.Close()
		cancel()
		frps.Close()
		t.Fatalf("register REST gateway: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		conn.Close()
		cancel()
		frps.Close()
		t.Fatalf("upstream listen: %v", err)
	}
	httpSrv := &http.Server{Handler: mux}
	go func() { _ = httpSrv.Serve(ln) }()

	providers := registry.New()
	if err := providers.Providers.Register("test-app", &mockProvider{name: "test-app"}); err != nil {
		cancel()
		frps.Close()
		conn.Close()
		t.Fatalf("register provider: %v", err)
	}

	h := &upstreamHarness{
		rmService: rmService,
		remoteCfg: &config.RemoteConfig{
			URL:   "http://" + ln.Addr().String(),
			Token: "test-token",
		},
		providers: &providers.Providers,
		frps:      frps,
		conn:      conn,
		httpLn:    ln,
		httpSrv:   httpSrv,
	}

	cleanup := func() {
		_ = httpSrv.Close()
		conn.Close()
		_ = frpsHTTP.Close()
		frps.Close()
		cancel()
	}
	return h, ctx, cleanup
}

func (h *upstreamHarness) publisherConfig() remotepublish.PublisherConfig {
	return remotepublish.PublisherConfig{
		Groups: []remotepublish.PublicationGroup{{
			RemoteName: "upstream",
			Remote:     h.remoteCfg,
			Providers: []remotepublish.ProviderPublication{{
				Kind:       "app",
				Name:       "test-app",
				Definition: map[string]any{"kind": "app", "name": "test-app"},
			}},
		}},
		Providers: h.providers,
	}
}

func TestPublisherPublishesAndShutsDown(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("publisher test requires in-process frps")
	}
	h, ctx, cleanup := newUpstreamHarness(t)
	defer cleanup()

	publisher := remotepublish.NewPublisher(h.publisherConfig())
	if err := publisher.Start(ctx); err != nil {
		t.Fatalf("publisher start: %v", err)
	}
	defer publisher.Shutdown(context.Background())

	waitReady(t, publisher, 30*time.Second)

	remotes, err := h.rmService.ListRemotes(adminContext(), &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("ListRemotes: %v", err)
	}
	if len(remotes.Remotes) != 1 || remotes.Remotes[0].Generation != 1 {
		t.Fatalf("expected 1 remote at generation 1, got %d remotes", len(remotes.Remotes))
	}

	publisher.Shutdown(context.Background())
	if !waitRemotesDeleted(t, h.rmService, 10*time.Second) {
		t.Fatalf("registration was not deleted on shutdown")
	}
}

func TestPublisherSameSubjectRestart(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("publisher test requires in-process frps")
	}
	h, ctx, cleanup := newUpstreamHarness(t)
	defer cleanup()

	// Generation 1: first publisher.
	pub1 := remotepublish.NewPublisher(h.publisherConfig())
	if err := pub1.Start(ctx); err != nil {
		t.Fatalf("pub1 start: %v", err)
	}
	waitReady(t, pub1, 30*time.Second)

	remotes, err := h.rmService.ListRemotes(adminContext(), &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("ListRemotes after gen1: %v", err)
	}
	if len(remotes.Remotes) != 1 || remotes.Remotes[0].Generation != 1 {
		t.Fatalf("expected 1 remote at generation 1, got %d remotes", len(remotes.Remotes))
	}
	gen1 := remotes.Remotes[0].Generation

	// Same-subject restart: pub2 lists, sees gen1, replaces at gen2.
	pub2 := remotepublish.NewPublisher(h.publisherConfig())
	if err := pub2.Start(ctx); err != nil {
		t.Fatalf("pub2 start: %v", err)
	}
	waitReady(t, pub2, 30*time.Second)

	remotes, err = h.rmService.ListRemotes(adminContext(), &proto.ListRemotesRequest{})
	if err != nil {
		t.Fatalf("ListRemotes after gen2: %v", err)
	}
	if len(remotes.Remotes) != 1 || remotes.Remotes[0].Generation != gen1+1 {
		t.Fatalf("expected generation %d, got %d", gen1+1, remotes.Remotes[0].Generation)
	}

	pub1.Shutdown(context.Background())
	pub2.Shutdown(context.Background())
}

func TestRegistrationLifecycleCheckExactSet(t *testing.T) {
	t.Parallel()
	providers := registry.New()
	if err := providers.Providers.Register("app-a", &mockProvider{name: "app-a"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	lookup := &providerLookupAdapter{providers: &providers.Providers}
	server := remotepublish.NewRegistrationLifecycleServerForTest([]string{"app-a"}, lookup)

	check := func(refs []*proto.ProviderRef) (bool, string, error) {
		resp, err := server.Check(context.Background(), &proto.RegistrationCheckRequest{Providers: refs})
		if err != nil {
			return false, "", err
		}
		return resp.Ready, resp.Message, nil
	}

	if ready, _, _ := check([]*proto.ProviderRef{{Kind: "app", Name: "app-a"}}); !ready {
		t.Fatalf("exact set should be ready")
	}
	if ready, msg, _ := check([]*proto.ProviderRef{{Kind: "app", Name: "app-a"}, {Kind: "app", Name: "app-b"}}); ready {
		t.Fatalf("superset should not be ready: %s", msg)
	}
	if ready, msg, _ := check([]*proto.ProviderRef{{Kind: "app", Name: "app-b"}}); ready {
		t.Fatalf("out-of-group provider should not be ready: %s", msg)
	}
	if _, _, err := check([]*proto.ProviderRef{{Kind: "workflow", Name: "app-a"}}); err == nil {
		t.Fatalf("wrong kind should return error")
	}
}

func TestBuildPublicationGroupsPlacementMatrix(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Server: config.ServerConfig{
			Remotes: map[string]*config.RemoteConfig{
				"upstream": {URL: "http://upstream.example", Token: "tok"},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"local-app":     {Remote: ""},
			"published-app": {Remote: "upstream", DevActive: true},
			"delegated-app": {Remote: "upstream"},
		},
	}

	groups, err := remotepublish.BuildPublicationGroups(cfg)
	if err != nil {
		t.Fatalf("BuildPublicationGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].RemoteName != "upstream" {
		t.Fatalf("expected upstream, got %s", groups[0].RemoteName)
	}
	if len(groups[0].Providers) != 1 {
		t.Fatalf("expected 1 published provider, got %d", len(groups[0].Providers))
	}
	if groups[0].Providers[0].Name != "published-app" {
		t.Fatalf("expected published-app, got %s", groups[0].Providers[0].Name)
	}
}

func TestBuildPublicationGroupsUnknownRemoteErrors(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Server: config.ServerConfig{
			Remotes: map[string]*config.RemoteConfig{
				"upstream": {URL: "http://upstream.example", Token: "tok"},
			},
		},
		Apps: map[string]*config.ProviderEntry{
			"bad-app": {Remote: "nonexistent", DevActive: true},
		},
	}
	_, err := remotepublish.BuildPublicationGroups(cfg)
	if err == nil {
		t.Fatalf("expected error for unknown remote name")
	}
}

func waitReady(t *testing.T, publisher *remotepublish.Publisher, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if publisher.ReadinessReason() == "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("publisher did not become ready within %v: %s", timeout, publisher.ReadinessReason())
}

func waitRemotesDeleted(t *testing.T, svc *remotemanagement.Service, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remotes, err := svc.ListRemotes(adminContext(), &proto.ListRemotesRequest{})
		if err == nil && len(remotes.Remotes) == 0 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func adminContext() context.Context {
	return principal.WithPrincipal(context.Background(), &principal.Principal{SubjectID: "user:alice@example.com"})
}

type providerLookupAdapter struct {
	providers *registry.ProviderMap[core.Provider]
}

func (l *providerLookupAdapter) Has(name string) bool {
	_, err := l.providers.Get(name)
	return err == nil
}

type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string                                            { return m.name }
func (m *mockProvider) DisplayName() string                                     { return m.name }
func (m *mockProvider) Description() string                                     { return "" }
func (m *mockProvider) ConnectionMode() core.ConnectionMode                     { return "" }
func (m *mockProvider) AuthTypes() []string                                     { return nil }
func (m *mockProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef { return nil }
func (m *mockProvider) CredentialFields() []core.CredentialFieldDef             { return nil }
func (m *mockProvider) DiscoveryConfig() *core.DiscoveryConfig                  { return nil }
func (m *mockProvider) ConnectionForOperation(string) string                    { return "" }
func (m *mockProvider) Catalog() *catalog.Catalog                               { return nil }
func (m *mockProvider) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, fmt.Errorf("not implemented")
}
