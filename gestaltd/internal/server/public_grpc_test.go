package server_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	appservice "github.com/valon-technologies/gestalt/server/services/apps"
	"github.com/valon-technologies/gestalt/server/services/apps/registry"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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

type plan6RelayInvoker struct {
	mu    sync.Mutex
	calls []relayTestInvokerCall
}

func (i *plan6RelayInvoker) Invoke(ctx context.Context, _ *principal.Principal, providerName, instance, operation string, params map[string]any) (*core.OperationResult, error) {
	i.mu.Lock()
	i.calls = append(i.calls, relayTestInvokerCall{
		calls:        len(i.calls) + 1,
		providerName: providerName,
		instance:     instance,
		operation:    operation,
	})
	i.mu.Unlock()
	return &core.OperationResult{Status: 202, Body: []byte("relayed")}, nil
}

func (i *plan6RelayInvoker) snapshot() []relayTestInvokerCall {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]relayTestInvokerCall, len(i.calls))
	copy(out, i.calls)
	return out
}

func plan6RemoteAppsConfig(localApps map[string]bool) *config.Config {
	apps := map[string]*config.ProviderEntry{
		"linear":        {},
		"valon-profile": {},
		"ci-cd":         {},
	}
	for name, active := range localApps {
		if entry := apps[name]; entry != nil {
			entry.DevActive = active
		}
	}
	return &config.Config{
		Server: config.ServerConfig{Remote: "https://remote.test"},
		Apps:   apps,
	}
}

func plan6BuildsLocal(cfg *config.Config, entry *config.ProviderEntry) bool {
	if entry == nil {
		return false
	}
	if entry.DevActive {
		return true
	}
	return cfg == nil || strings.TrimSpace(cfg.Server.Remote) == ""
}

func plan6LocalAppStub(name string) *coretesting.StubIntegration {
	return &coretesting.StubIntegration{
		N:        name,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Operations: []catalog.CatalogOperation{{ID: "ping"}},
		},
		ExecuteFn: func(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
			return &core.OperationResult{Status: 201, Body: []byte("local")}, nil
		},
	}
}

func plan6RemoteAppStubs() []core.Provider {
	return []core.Provider{
		&coretesting.StubIntegration{
			N:        "linear",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Operations: []catalog.CatalogOperation{{ID: "issues.list"}},
			},
		},
		&coretesting.StubIntegration{
			N:        "valon-profile",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Operations: []catalog.CatalogOperation{{ID: "issues.list"}},
			},
		},
	}
}

func startPlan6RemotePublicGRPCServer(t *testing.T) (*httptest.Server, *plan6RelayInvoker) {
	t.Helper()
	invoker := &plan6RelayInvoker{}
	ts := httptest.NewUnstartedServer(newTestHandler(t, func(cfg *server.Config) {
		configurePublicGRPCTestServer(t, cfg, nil)
		cfg.Invoker = invoker
		cfg.Providers = testutil.NewProviderRegistry(t, plan6RemoteAppStubs()...)
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	testutil.CloseOnCleanup(t, ts)
	return ts, invoker
}

func plan6RemoteClientSet(t *testing.T, ts *httptest.Server, scope string) *remote.ClientSet {
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

func registerPlan6RemoteApps(reg *registry.ProviderMap[core.Provider], cfg *config.Config, client proto.AppClient) error {
	for name, entry := range cfg.Apps {
		if entry == nil || plan6BuildsLocal(cfg, entry) {
			continue
		}
		spec := appservice.StaticProviderSpec{
			Name:           name,
			ConnectionMode: core.ConnectionModeNone,
			Catalog:        &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "issues.list"}, {ID: "ping"}}},
		}
		provider := appservice.NewGestaltRemote(client, spec)
		if provider == nil {
			return fmt.Errorf("remote app %q: provider client is required", name)
		}
		if err := reg.Register(name, provider); err != nil {
			return fmt.Errorf("remote app %q: %w", name, err)
		}
	}
	return nil
}

func newPlan6Broker(t *testing.T, cfg *config.Config, clients *remote.ClientSet, localApps ...core.Provider) *invocation.Broker {
	t.Helper()
	reg := testutil.NewProviderRegistry(t, localApps...)
	if err := registerPlan6RemoteApps(reg, cfg, clients.App); err != nil {
		t.Fatalf("registerPlan6RemoteApps: %v", err)
	}
	svc := testutil.NewStubServices(t)
	return invocation.NewBroker(reg, svc.Users, svc.ExternalCredentials)
}

func plan6Principal(scopes ...string) *principal.Principal {
	return &principal.Principal{
		SubjectID: "user:dev@example.com",
		Kind:      principal.KindUser,
		Scopes:    scopes,
	}
}

func invokePlan6App(t *testing.T, broker *invocation.Broker, app, operation string) *core.OperationResult {
	t.Helper()
	result, err := broker.Invoke(context.Background(), plan6Principal(app), app, "", operation, nil)
	if err != nil {
		t.Fatalf("Invoke(%q): %v", app, err)
	}
	return result
}

func TestPlan6RemoteAppRoutingLifecycles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		localApps     map[string]bool
		localStubs    []core.Provider
		localChecks   []struct{ app, operation string }
		remoteChecks  []struct{ app, operation string }
		wantRemoteApp string
		wantRemoteN   int
	}{
		{
			name:      "nothing local",
			localApps: nil,
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
				{"valon-profile", "issues.list"},
			},
			wantRemoteN: 2,
		},
		{
			name:       "ci-cd local",
			localApps:  map[string]bool{"ci-cd": true},
			localStubs: []core.Provider{plan6LocalAppStub("ci-cd")},
			localChecks: []struct{ app, operation string }{
				{"ci-cd", "ping"},
			},
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
				{"valon-profile", "issues.list"},
			},
			wantRemoteN: 2,
		},
		{
			name:      "ci-cd and valon-profile local",
			localApps: map[string]bool{"ci-cd": true, "valon-profile": true},
			localStubs: []core.Provider{
				plan6LocalAppStub("ci-cd"),
				plan6LocalAppStub("valon-profile"),
			},
			localChecks: []struct{ app, operation string }{
				{"valon-profile", "ping"},
			},
			remoteChecks: []struct{ app, operation string }{
				{"linear", "issues.list"},
			},
			wantRemoteApp: "linear",
			wantRemoteN:   1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			remoteTS, invoker := startPlan6RemotePublicGRPCServer(t)
			clients := plan6RemoteClientSet(t, remoteTS, "linear")
			broker := newPlan6Broker(t, plan6RemoteAppsConfig(tc.localApps), clients, tc.localStubs...)

			for _, check := range tc.localChecks {
				result := invokePlan6App(t, broker, check.app, check.operation)
				if result.Status != 201 || string(result.Body) != "local" {
					t.Fatalf("Invoke(%q) = %#v, want local 201", check.app, result)
				}
			}
			for _, check := range tc.remoteChecks {
				result := invokePlan6App(t, broker, check.app, check.operation)
				if result.Status != 202 || string(result.Body) != "relayed" {
					t.Fatalf("Invoke(%q) = %#v, want remote 202 relayed", check.app, result)
				}
			}

			calls := invoker.snapshot()
			if len(calls) != tc.wantRemoteN {
				t.Fatalf("remote invoker calls = %d, want %d", len(calls), tc.wantRemoteN)
			}
			if tc.wantRemoteApp != "" && (len(calls) != 1 || calls[0].providerName != tc.wantRemoteApp) {
				t.Fatalf("remote invoker calls = %#v, want %q only", calls, tc.wantRemoteApp)
			}
		})
	}
}

func TestPlan6RemoteFailureSemantics(t *testing.T) {
	t.Parallel()

	t.Run("undeclared provider remains not found", func(t *testing.T) {
		t.Parallel()

		remoteTS, _ := startPlan6RemotePublicGRPCServer(t)
		clients := plan6RemoteClientSet(t, remoteTS, "linear")
		broker := newPlan6Broker(t, plan6RemoteAppsConfig(nil), clients)

		_, err := broker.Invoke(context.Background(), plan6Principal("missing"), "missing", "", "op", nil)
		if !errors.Is(err, invocation.ErrProviderNotFound) {
			t.Fatalf("err = %v, want ErrProviderNotFound", err)
		}
	})

	t.Run("dev active does not fall back to remote", func(t *testing.T) {
		t.Parallel()

		remoteTS, invoker := startPlan6RemotePublicGRPCServer(t)
		clients := plan6RemoteClientSet(t, remoteTS, "linear")
		cfg := plan6RemoteAppsConfig(map[string]bool{"linear": true})
		broker := newPlan6Broker(t, cfg, clients)

		_, err := broker.Invoke(context.Background(), plan6Principal("linear"), "linear", "", "issues.list", nil)
		if !errors.Is(err, invocation.ErrProviderNotFound) {
			t.Fatalf("err = %v, want ErrProviderNotFound without local provider", err)
		}
		if len(invoker.snapshot()) != 0 {
			t.Fatalf("remote invoker calls = %d, want 0", len(invoker.snapshot()))
		}
	})

	t.Run("wrong remote token surfaces auth error", func(t *testing.T) {
		t.Parallel()

		remoteTS, invoker := startPlan6RemotePublicGRPCServer(t)
		clients, err := remote.NewClientSet(context.Background(), remote.Config{
			URL:       remoteTS.URL,
			Token:     "wrong-token",
			TLSConfig: &tls.Config{InsecureSkipVerify: true},
		})
		if err != nil {
			t.Fatalf("NewClientSet: %v", err)
		}
		t.Cleanup(func() { _ = clients.Close() })

		reg := testutil.NewProviderRegistry(t)
		spec := appservice.StaticProviderSpec{
			Name:           "linear",
			ConnectionMode: core.ConnectionModeNone,
			Catalog:        &catalog.Catalog{Operations: []catalog.CatalogOperation{{ID: "issues.list"}}},
		}
		if err := reg.Register("linear", appservice.NewGestaltRemote(clients.App, spec)); err != nil {
			t.Fatalf("Register linear: %v", err)
		}
		svc := testutil.NewStubServices(t)
		broker := invocation.NewBroker(reg, svc.Users, svc.ExternalCredentials)

		_, err = broker.Invoke(context.Background(), plan6Principal("linear"), "linear", "", "issues.list", nil)
		if err == nil {
			t.Fatal("expected auth error, got nil")
		}
		if !errors.Is(err, invocation.ErrNotAuthenticated) {
			t.Fatalf("err = %v, want ErrNotAuthenticated", err)
		}
		if len(invoker.snapshot()) != 0 {
			t.Fatalf("remote invoker calls = %d, want 0", len(invoker.snapshot()))
		}
	})
}
