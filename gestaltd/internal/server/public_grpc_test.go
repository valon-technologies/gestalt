package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPublicGRPCAppInvokeWithBearerToken(t *testing.T) {
	t.Parallel()

	invoker := &relayTestInvoker{}
	plaintext := scopedTestBearerToken("public-user", "")

	ts := httptestNewPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.AppInvocation = invoker
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+plaintext))

	_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
	})
	if err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
	if call := invoker.snapshot(); call.calls != 1 || call.providerName != "slack" {
		t.Fatalf("invoker call = %+v, want slack invocation", call)
	}
}

func TestPublicGRPCAppInvokeRejectsRunAs(t *testing.T) {
	t.Parallel()

	plaintext := scopedTestBearerToken("public-user", "")
	ts := httptestNewPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.AppInvocation = &relayTestInvoker{}
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+plaintext))

	_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
		RunAs:     &proto.SubjectContext{Id: "user:other"},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("status code = %v, want InvalidArgument (%v)", grpcstatus.Code(err), err)
	}
}

func TestPublicGRPCAppInvokeRejectsClientContext(t *testing.T) {
	t.Parallel()

	plaintext := scopedTestBearerToken("public-user", "")
	ts := httptestNewPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.AppInvocation = &relayTestInvoker{}
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+plaintext))

	_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
		Context:   relayAppRequestContext(),
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("status code = %v, want InvalidArgument (%v)", grpcstatus.Code(err), err)
	}
}

func TestPublicGRPCRejectsNonPublicMethod(t *testing.T) {
	t.Parallel()

	plaintext := scopedTestBearerToken("public-user", "")
	ts := httptestNewPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
	})
	conn := newRelayGRPCConn(t, ts)
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+plaintext))

	_, err := proto.NewWorkflowClient(conn).ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{})
	if grpcstatus.Code(err) != codes.Unimplemented {
		t.Fatalf("status code = %v, want Unimplemented (%v)", grpcstatus.Code(err), err)
	}
}

func TestHostServiceRelayStillAllowsTrustedContext(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
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

	ts := httptestNewPublicGRPCServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubForScopedBearer()
		cfg.AppInvocation = invoker
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	})

	tokenManager, err := runtimehost.NewHostServiceRelayTokenManager(secret)
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
	if call := invoker.snapshot(); call.calls != 1 {
		t.Fatalf("invoker calls = %d, want relay path to dispatch", call.calls)
	}
}

func httptestNewPublicGRPCServer(t *testing.T, opts ...func(*server.Config)) *httptest.Server {
	t.Helper()
	ts := newTestHTTPServer(t, func(h http.Handler) *httptest.Server {
		srv := httptest.NewUnstartedServer(h)
		srv.EnableHTTP2 = true
		srv.StartTLS()
		return srv
	}, func(cfg *server.Config) {
		cfg.RouteProfile = server.RouteProfilePublic
		cfg.StateSecret = []byte("0123456789abcdef0123456789abcdef")
		if cfg.PublicHostServices == nil {
			cfg.PublicHostServices = runtimehost.NewPublicHostServiceRegistry()
		}
		for _, opt := range opts {
			opt(cfg)
		}
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}
