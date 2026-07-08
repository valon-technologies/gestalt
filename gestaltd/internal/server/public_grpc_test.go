package server_test

import (
	"context"
	"crypto/tls"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var publicGRPCTestSecret = []byte("public-grpc-test-secret-01234567")

func configurePublicGRPCTestServer(t *testing.T, cfg *server.Config, invoker *relayTestInvoker) {
	t.Helper()
	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	transport := providergateway.NewProviderGatewayTransport()
	authz := &serviceAccountCredentialAuthorizationProvider{allowed: true}
	transport.SetIdentityProvider(testAuthStubForScopedBearer())
	transport.SetPublicMethods(registry)
	transport.SetAuthorizationProvider(authz)
	transport.SetPublicBaseURL("https://gestalt.test")
	cfg.Auth = testAuthStubForScopedBearer()
	cfg.Authorization = authz
	cfg.PublicBaseURL = "https://gestalt.test"
	cfg.PublicGatewayTransport = transport
	cfg.StateSecret = publicGRPCTestSecret
	if invoker != nil {
		cfg.Invoker = invoker
	}
}

func publicGRPCTestBearer(scope string) string {
	return scopedTestBearerToken("public-grpc-user", scope)
}

func startPublicGRPCServer(t *testing.T, configure func(*server.Config)) (*httptest.Server, *relayTestInvoker) {
	t.Helper()
	invoker := &relayTestInvoker{}
	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		configurePublicGRPCTestServer(t, cfg, invoker)
		if configure != nil {
			configure(cfg)
		}
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)
	return ts, invoker
}

func startExamplePublicGRPCServer(t *testing.T) (*httptest.Server, *relayTestInvoker) {
	t.Helper()
	return startPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "example",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "example",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: "POST"}},
			},
		})
	})
}

// TestPublicGRPCRouting verifies public gRPC wiring end-to-end: bearer-authenticated
// gRPC is handled by the public gateway surface, while host-service relay traffic
// continues through the trusted relay path. Individual RPC policy and per-service
// behavior are covered by providergateway and publicrpc unit tests.
func TestPublicGRPCRouting(t *testing.T) {
	t.Parallel()

	t.Run("bearer routes through public gateway", func(t *testing.T) {
		t.Parallel()

		ts, invoker := startExamplePublicGRPCServer(t)
		clients, err := remote.NewClientSet(context.Background(), remote.Config{
			URL:       ts.URL,
			Token:     publicGRPCTestBearer("example"),
			TLSConfig: &tls.Config{InsecureSkipVerify: true},
		})
		if err != nil {
			t.Fatalf("remote.NewClientSet: %v", err)
		}
		defer func() { _ = clients.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := clients.App.Invoke(ctx, &proto.AppInvokeRequest{
			App:       "example",
			Operation: "sync",
		}); err != nil {
			t.Fatalf("remote App.Invoke: %v", err)
		}
		if call := invoker.snapshot(); call.calls != 1 || call.providerName != "example" || call.operation != "sync" {
			t.Fatalf("invoker call = %+v, want example/sync", call)
		}
	})

	for _, tc := range []struct {
		name     string
		metadata []string
	}{
		{name: "missing bearer is rejected"},
		{name: "invalid bearer scheme is rejected", metadata: []string{"authorization", "Basic not-a-bearer-token"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts, invoker := startExamplePublicGRPCServer(t)
			conn := newRelayGRPCConn(t, ts)
			defer func() { _ = conn.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if len(tc.metadata) > 0 {
				ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(tc.metadata...))
			}

			_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
				App:       "example",
				Operation: "sync",
			})
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("public App.Invoke code = %v, want %v (%v)", status.Code(err), codes.Unauthenticated, err)
			}
			if call := invoker.snapshot(); call.calls != 0 {
				t.Fatalf("invoker calls = %d, want 0", call.calls)
			}
		})
	}

	t.Run("relay token bypasses public gateway", func(t *testing.T) {
		t.Parallel()

		invoker := &relayTestInvoker{}
		publicHostServices := runtimehost.NewPublicHostServiceRegistry()
		sessionVerifier := newRelayTestSessionVerifier("relay-session")
		publicHostServices.RegisterVerified("support", sessionVerifier, runtimehost.HostService{
			Name:           "app",
			MethodPrefixes: []string{"/" + proto.App_ServiceDesc.ServiceName + "/"},
			Register: func(srv *grpc.Server) {
				proto.RegisterAppServer(srv, appaccessservice.NewServer(
					invoker,
					appaccessservice.WithCallerApp("support"),
				))
			},
		})

		ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
			configurePublicGRPCTestServer(t, cfg, invoker)
			cfg.RouteProfile = server.RouteProfilePublic
			cfg.PublicBaseURL = "https://gestalt.test"
			cfg.ManagementBaseURL = "https://gestalt.test"
			cfg.PublicHostServices = publicHostServices
		}))
		ts.EnableHTTP2 = true
		ts.StartTLS()
		testutil.CloseOnCleanup(t, ts)

		tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(publicGRPCTestSecret)
		if err != nil {
			t.Fatalf("NewHostServiceRelayTokenManager: %v", err)
		}
		relayToken, err := tokenManager.MintToken(runtimehost.HostServiceRelayTokenRequest{
			AppName:      "support",
			SessionID:    "relay-session",
			Service:      "app",
			MethodPrefix: "/" + proto.App_ServiceDesc.ServiceName + "/",
			TTL:          time.Minute,
		})
		if err != nil {
			t.Fatalf("MintToken: %v", err)
		}

		conn := newRelayGRPCConn(t, ts)
		defer func() { _ = conn.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(runtimehost.HostServiceRelayTokenHeader, relayToken))
		_, err = proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
			Context:   relayAppRequestContext(),
			App:       "slack",
			Operation: "events.reply",
		})
		if err != nil {
			t.Fatalf("relay App.Invoke: %v", err)
		}
		if call := invoker.snapshot(); call.calls != 1 || call.providerName != "slack" {
			t.Fatalf("relay invoker call = %+v, want slack", call)
		}
	})
}
