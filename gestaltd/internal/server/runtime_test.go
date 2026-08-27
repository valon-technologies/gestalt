package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestAppRegistryRestartDelay(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		raw         string
		want        time.Duration
		wantDisable bool
		wantErr     bool
	}{
		{name: "production default", wantDisable: true},
		{name: "early rollout", raw: "1m", want: time.Minute},
		{name: "explicit zero is invalid", raw: "0s", wantErr: true},
		{name: "invalid", raw: "later", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			cfg.Server.AppRegistry.RestartDelay = tc.raw
			got, disabled, err := appRegistryRestartDelay(cfg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("appRegistryRestartDelay error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want || disabled != tc.wantDisable {
				t.Fatalf("appRegistryRestartDelay = (%v, %v), want (%v, %v)", got, disabled, tc.want, tc.wantDisable)
			}
		})
	}
}

func TestHTTPCatalogConnectionMapUsesAPIConnection(t *testing.T) {
	t.Parallel()

	connMaps := bootstrap.ConnectionMaps{
		DefaultConnection: map[string]string{"notion": "OAuth"},
		APIConnection:     map[string]string{"notion": "OAuth"},
		MCPConnection:     map[string]string{"notion": "MCP"},
	}

	got := httpCatalogConnectionMap(connMaps)
	if got["notion"] != "OAuth" {
		t.Fatalf("catalog connection = %q, want %q", got["notion"], "OAuth")
	}
}

type fakeIndexedDBPinger struct {
	err error
}

func (p fakeIndexedDBPinger) Ping(context.Context) error {
	return p.err
}

func TestRuntimeReadinessStatusWaitsForWorkflowProviders(t *testing.T) {
	t.Parallel()

	workflowProvidersReady := make(chan struct{})
	check := runtimeReadinessStatus(workflowProvidersReady, fakeIndexedDBPinger{})

	if got := check(); got != "workflow providers loading" {
		t.Fatalf("readiness before workflow providers = %q, want %q", got, "workflow providers loading")
	}

	close(workflowProvidersReady)
	if got := check(); got != "" {
		t.Fatalf("readiness after workflow providers = %q, want ready", got)
	}
}

func TestRuntimeReadinessStatusChecksIndexedDBAfterProviders(t *testing.T) {
	t.Parallel()

	workflowProvidersReady := make(chan struct{})
	close(workflowProvidersReady)
	check := runtimeReadinessStatus(
		workflowProvidersReady,
		fakeIndexedDBPinger{err: errors.New("down")},
	)

	if got := check(); got != "indexeddb unavailable" {
		t.Fatalf("readiness with indexeddb error = %q, want %q", got, "indexeddb unavailable")
	}
}

type gatedWorkflowProvider struct {
	coreworkflow.Provider
	started chan struct{}
	gate    chan struct{}
}

func (p *gatedWorkflowProvider) Start(ctx context.Context) error {
	close(p.started)
	select {
	case <-p.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestServeRuntimeReadyAfterWorkflowProvidersStart(t *testing.T) {
	t.Parallel()

	provider := &gatedWorkflowProvider{
		started: make(chan struct{}),
		gate:    make(chan struct{}),
	}
	result := &bootstrap.Result{
		ExtraWorkflows: []coreworkflow.Provider{provider},
	}

	workflowProvidersReady := make(chan struct{})
	ready := runtimeReadinessStatus(workflowProvidersReady, fakeIndexedDBPinger{})

	servers := []namedHTTPServer{{
		name:   "public",
		server: newHTTPServer("127.0.0.1:0", http.NewServeMux()),
	}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = serveRuntime(ctx, &config.Config{}, bootstrap.ConnectionMaps{}, result, nil, servers, &switchableHandler{}, workflowProvidersReady, nil, nil, nil)
	}()

	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("workflow provider Start was never invoked")
	}

	if got := ready(); got != "workflow providers loading" {
		t.Fatalf("readiness while workflow provider starting = %q, want %q", got, "workflow providers loading")
	}

	close(provider.gate)

	deadline := time.After(5 * time.Second)
	for ready() != "" {
		select {
		case <-deadline:
			t.Fatal("readiness never became ready after workflow providers started")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	<-serveDone
}

type runtimeTestCacheServer struct {
	proto.UnimplementedCacheServer

	mu   sync.Mutex
	keys []string
}

func (s *runtimeTestCacheServer) Get(_ context.Context, req *proto.CacheGetRequest) (*proto.CacheGetResponse, error) {
	s.mu.Lock()
	s.keys = append(s.keys, req.GetKey())
	s.mu.Unlock()
	return &proto.CacheGetResponse{
		Found: true,
		Value: []byte("relay:" + req.GetKey()),
	}, nil
}

func TestNewHTTPServerSupportsH2CHostServiceRelay(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	cacheSrv := &runtimeTestCacheServer{}
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	publicHostServices.RegisterVerified("relay-plugin", allowHostServiceSessionVerifier{}, runtimehost.HostService{
		Name:           "cache",
		MethodPrefixes: []string{"/gestalt.provider.v1.Cache/"},
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	})

	reg := registry.New()
	services := testutil.NewStubServices(t)
	handler, err := New(Config{
		Auth:               &coretesting.StubAuthProvider{N: "none"},
		Services:           services,
		Providers:          &reg.Providers,
		StateSecret:        secret,
		RouteProfile:       RouteProfilePublic,
		Invoker:            invocation.NewBroker(&reg.Providers, services.Users, services.ExternalCredentials),
		PublicHostServices: publicHostServices,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	httpServer := newHTTPServer(listener.Addr().String(), handler)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- httpServer.Serve(listener)
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		if err := <-serverDone; err != nil && err != http.ErrServerClosed {
			t.Fatalf("Serve: %v", err)
		}
	})

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
		AppName:      "relay-plugin",
		SessionID:    "session-1",
		Service:      "cache",
		MethodPrefix: "/" + proto.Cache_ServiceDesc.ServiceName + "/",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	relayCtx := invocation.WithAgentInvocationContext(context.Background(), invocation.AgentInvocationContext{
		SessionID: "session-1",
	})

	ctx, cancel := context.WithTimeout(relayCtx, 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		runtimehost.HostServiceRelayTokenHeader, token,
	))

	resp, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if err != nil {
		t.Fatalf("Cache.Get via h2c relay: %v", err)
	}
	if got := string(resp.GetValue()); got != "relay:hello" {
		t.Fatalf("Cache.Get value = %q, want %q", got, "relay:hello")
	}
}
