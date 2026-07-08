package server_test

import (
	"context"
	"crypto/tls"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
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

func publicGRPCRemoteClientSet(t *testing.T, ts *httptest.Server, scope string) *remote.ClientSet {
	t.Helper()
	clients, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:       ts.URL,
		Token:     publicGRPCTestBearer(scope),
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatalf("remote.NewClientSet: %v", err)
	}
	t.Cleanup(func() { _ = clients.Close() })
	return clients
}

func remoteRoutingAppStub(name, operation string) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: operation}},
		},
	}
}

// TestPublicGRPCRouting verifies public gRPC wiring end-to-end: bearer-authenticated
// gRPC is handled by the public gateway surface, while host-service relay traffic
// continues through the trusted relay path. Individual RPC policy and per-service
// behavior are covered by providergateway and publicrpc unit tests.
func TestPublicGRPCRouting(t *testing.T) {
	t.Parallel()

	t.Run("bearer routes public gateway services", func(t *testing.T) {
		t.Parallel()

		stubDB := &coretesting.StubIndexedDB{}
		ts, invoker := startPublicGRPCServer(t, func(cfg *server.Config) {
			cfg.Providers = testutil.NewProviderRegistry(t,
				remoteRoutingAppStub("linear", "issues.list"),
				remoteRoutingAppStub("valon-profile", "issues.list"),
			)
			cfg.IndexedDBs = map[string]coreindexeddb.IndexedDB{"main-db": stubDB}
			cfg.SelectedIndexedDBName = "main-db"
		})
		clients := publicGRPCRemoteClientSet(t, ts, "linear")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, app := range []string{"linear", "valon-profile"} {
			if _, err := clients.App.Invoke(ctx, &proto.AppInvokeRequest{
				App:       app,
				Operation: "issues.list",
			}); err != nil {
				t.Fatalf("remote App.Invoke(%q): %v", app, err)
			}
		}

		calls := invoker.snapshots()
		if len(calls) != 2 {
			t.Fatalf("invoker calls = %d, want 2", len(calls))
		}
		for i, want := range []struct{ provider, operation string }{
			{"linear", "issues.list"},
			{"valon-profile", "issues.list"},
		} {
			if calls[i].providerName != want.provider || calls[i].operation != want.operation {
				t.Fatalf("invoker call[%d] = %+v, want %s/%s", i, calls[i], want.provider, want.operation)
			}
		}

		recordValue, err := indexeddbcodec.RecordToProto(indexeddbcodec.Record{"id": "task-1", "value": "ship-it"})
		if err != nil {
			t.Fatalf("RecordToProto: %v", err)
		}
		idbCtx := metadata.AppendToOutgoingContext(ctx, runtimehost.HostServiceBindingHeader, "main-db")
		if _, err := clients.IndexedDB.Put(idbCtx, &proto.RecordRequest{
			Store:  "tasks",
			Record: recordValue,
		}); err != nil {
			t.Fatalf("remote IndexedDB.Put: %v", err)
		}
		got, err := stubDB.ObjectStore("tasks").Get(ctx, "task-1")
		if err != nil {
			t.Fatalf("stub IndexedDB.Get: %v", err)
		}
		if got["value"] != "ship-it" {
			t.Fatalf("stored value = %#v, want %q", got["value"], "ship-it")
		}
	})

	for _, tc := range []struct {
		name        string
		metadata    []string
		remoteToken string
	}{
		{name: "missing bearer is rejected"},
		{name: "invalid bearer scheme is rejected", metadata: []string{"authorization", "Basic not-a-bearer-token"}},
		{name: "invalid bearer via remote client is rejected", remoteToken: "wrong-token"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts, invoker := startExamplePublicGRPCServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var err error
			if tc.remoteToken != "" {
				clients, clientErr := remote.NewClientSet(context.Background(), remote.Config{
					URL:       ts.URL,
					Token:     tc.remoteToken,
					TLSConfig: &tls.Config{InsecureSkipVerify: true},
				})
				if clientErr != nil {
					t.Fatalf("remote.NewClientSet: %v", clientErr)
				}
				t.Cleanup(func() { _ = clients.Close() })
				_, err = clients.App.Invoke(ctx, &proto.AppInvokeRequest{
					App:       "example",
					Operation: "sync",
				})
			} else {
				conn := newRelayGRPCConn(t, ts)
				defer func() { _ = conn.Close() }()
				if len(tc.metadata) > 0 {
					ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(tc.metadata...))
				}
				_, err = proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
					App:       "example",
					Operation: "sync",
				})
			}

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
