package server

import (
	"context"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/registry"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

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
	const envVar = "GESTALT_TEST_CACHE_SOCKET"
	publicHostServices := providerhost.NewPublicHostServiceRegistry()
	publicHostServices.Register("relay-plugin", providerhost.HostService{
		Name:   "cache",
		EnvVar: envVar,
		Register: func(srv *grpc.Server) {
			proto.RegisterCacheServer(srv, cacheSrv)
		},
	})

	reg := registry.New()
	services := coretesting.NewStubServices(t)
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

	tokenManager, err := providerhost.NewHostServiceRelayTokenManager(secret)
	if err != nil {
		t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
	}
	token, err := tokenManager.MintToken(providerhost.HostServiceRelayTokenRequest{
		PluginName:   "relay-plugin",
		SessionID:    "session-1",
		Service:      "cache",
		EnvVar:       envVar,
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(providerhost.HostServiceRelayTokenHeader, token))

	resp, err := proto.NewCacheClient(conn).Get(ctx, &proto.CacheGetRequest{Key: "hello"})
	if err != nil {
		t.Fatalf("Cache.Get via h2c relay: %v", err)
	}
	if got := string(resp.GetValue()); got != "relay:hello" {
		t.Fatalf("Cache.Get value = %q, want %q", got, "relay:hello")
	}
}
