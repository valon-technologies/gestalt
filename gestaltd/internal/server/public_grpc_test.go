package server_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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

func publicGRPCContext(t *testing.T, bearerToken string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+bearerToken))
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

func TestPublicGRPCAppInvokeSucceedsWithBearerAuth(t *testing.T) {
	t.Parallel()

	ts, invoker := startPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: "POST"}},
			},
		})
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	_, err := proto.NewAppClient(conn).Invoke(publicGRPCContext(t, publicGRPCTestBearer("roadmap")), &proto.AppInvokeRequest{
		App:       "roadmap",
		Operation: "sync",
	})
	if err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
	if call := invoker.snapshot(); call.calls != 1 || call.providerName != "roadmap" || call.operation != "sync" {
		t.Fatalf("invoker call = %+v, want roadmap/sync", call)
	}
}

func TestPublicGRPCRelayInvokeStillWorksWithTrustedContext(t *testing.T) {
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
}

func TestPublicGRPCWorkflowDeliverEventSucceeds(t *testing.T) {
	t.Parallel()

	provider := newMemoryWorkflowProvider()
	ts, _ := startPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "roadmap",
			ConnMode: core.ConnectionModeNone,
			CatalogVal: &catalog.Catalog{
				Name:       "roadmap",
				Operations: []catalog.CatalogOperation{{ID: "sync", Method: "POST"}},
			},
		})
		cfg.Workflow = &stubWorkflowControl{defaultProviderName: "basic", provider: provider}
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	_, err := proto.NewWorkflowClient(conn).DeliverEvent(publicGRPCContext(t, publicGRPCTestBearer("roadmap")), &proto.DeliverWorkflowProviderEventRequest{
		AppName: "roadmap",
		Event:   &proto.WorkflowEvent{Source: "roadmap", Type: "roadmap.item.updated"},
	})
	if err != nil {
		t.Fatalf("Workflow.DeliverEvent: %v", err)
	}
	if len(provider.deliveredEvents) != 1 {
		t.Fatalf("delivered events = %d, want 1", len(provider.deliveredEvents))
	}
}

func TestPublicGRPCAgentCreateSessionSucceeds(t *testing.T) {
	t.Parallel()

	ts, _ := startPublicGRPCServer(t, func(cfg *server.Config) {
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: newMemoryAgentProvider()}
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	_, err := proto.NewAgentClient(conn).CreateSession(publicGRPCContext(t, publicGRPCTestBearer("")), &proto.CreateAgentProviderSessionRequest{
		Model: "gpt-test",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}
