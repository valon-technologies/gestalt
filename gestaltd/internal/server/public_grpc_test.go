package server_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	appaccessservice "github.com/valon-technologies/gestalt/server/services/appaccess"
	"github.com/valon-technologies/gestalt/server/services/agents/agentmanager"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

func TestPublicGRPCAppInvokeSucceedsWithBearer(t *testing.T) {
	t.Parallel()

	invoker := &relayTestInvoker{}
	conn, _ := startPublicGRPCTestServer(t, invoker, publicGRPCTestAuth())
	ctx := bearerContext(context.Background(), "valid-token")
	_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
		Instance:  "prod",
	})
	if err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
	call := invoker.snapshot()
	if call.calls != 1 || call.providerName != "slack" || call.operation != "events.reply" {
		t.Fatalf("invoker call = %+v", call)
	}
}

func TestPublicGRPCAppInvokeRejectsRunAs(t *testing.T) {
	t.Parallel()

	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth())
	ctx := bearerContext(context.Background(), "valid-token")
	_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
		RunAs:     &proto.SubjectContext{Id: "user:other"},
	})
	assertPublicGRPCCode(t, err, codes.InvalidArgument)
}

func TestPublicGRPCAppInvokeRejectsClientContext(t *testing.T) {
	t.Parallel()

	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth())
	ctx := bearerContext(context.Background(), "valid-token")
	_, err := proto.NewAppClient(conn).Invoke(ctx, &proto.AppInvokeRequest{
		App:       "slack",
		Operation: "events.reply",
		Context:   relayAppRequestContext(),
	})
	assertPublicGRPCCode(t, err, codes.InvalidArgument)
}

func TestPublicGRPCRelayAppInvokeStillAcceptsTrustedContext(t *testing.T) {
	t.Parallel()

	secret := []byte("relay-test-secret-0123456789abcd")
	invoker := &relayTestInvoker{}
	publicHostServices := runtimehost.NewPublicHostServiceRegistry()
	publicHostServices.RegisterVerified("support", newRelayTestSessionVerifier("relay-session"), runtimehost.HostService{
		Name:           "app",
		MethodPrefixes: []string{"/" + proto.App_ServiceDesc.ServiceName + "/"},
		Register: func(srv *grpc.Server) {
			proto.RegisterAppServer(srv, appaccessservice.NewServer(
				invoker,
				appaccessservice.WithCallerApp("support"),
			))
		},
	})

	handler, listener := newPublicGRPCTestHandler(t, invoker, publicGRPCTestAuth(), func(cfg *server.Config) {
		cfg.StateSecret = secret
		cfg.PublicHostServices = publicHostServices
	})
	startPublicGRPCTestListener(t, handler, listener)

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

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

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
		t.Fatalf("invoker calls = %d, want 1", call.calls)
	}
}

func TestPublicGRPCWorkflowDeliverEventSucceeds(t *testing.T) {
	t.Parallel()

	provider := newMemoryWorkflowProvider()
	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth(), func(cfg *server.Config) {
		cfg.Workflow = &stubWorkflowControl{
			defaultProviderName: "basic",
			provider:            provider,
		}
	})
	ctx := bearerContext(context.Background(), "valid-token")
	_, err := proto.NewWorkflowClient(conn).DeliverEvent(ctx, &proto.DeliverWorkflowProviderEventRequest{
		ProviderName: "basic",
		Event: &proto.WorkflowEvent{
			Source: "roadmap",
			Type:   "roadmap.item.updated",
		},
	})
	if err != nil {
		t.Fatalf("Workflow.DeliverEvent: %v", err)
	}
	if len(provider.deliveredEvents) != 1 {
		t.Fatalf("delivered events = %d, want 1", len(provider.deliveredEvents))
	}
}

func TestPublicGRPCWorkflowApplyDefinitionNotRegistered(t *testing.T) {
	t.Parallel()

	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth())
	ctx := bearerContext(context.Background(), "valid-token")
	_, err := proto.NewWorkflowClient(conn).ApplyDefinition(ctx, &proto.ApplyWorkflowProviderDefinitionRequest{
		ProviderName: "basic",
	})
	assertPublicGRPCCode(t, err, codes.Unimplemented)
}

func TestPublicGRPCReflectionOmitsInternalWorkflowMethods(t *testing.T) {
	t.Parallel()

	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		t.Fatalf("ServerReflectionInfo: %v", err)
	}
	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{},
	}); err != nil {
		t.Fatalf("Send list services: %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv list services: %v", err)
	}
	services := resp.GetListServicesResponse().GetService()
	var workflowService string
	for _, service := range services {
		if strings.HasSuffix(service.GetName(), ".Workflow") {
			workflowService = service.GetName()
			break
		}
	}
	if workflowService == "" {
		t.Fatalf("workflow service missing from %v", services)
	}

	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: workflowService,
		},
	}); err != nil {
		t.Fatalf("Send file containing symbol: %v", err)
	}
	resp, err = stream.Recv()
	if err != nil {
		t.Fatalf("Recv file containing symbol: %v", err)
	}
	descriptor := string(resp.GetFileDescriptorResponse().GetFileDescriptorProto()[0])
	if !strings.Contains(descriptor, "DeliverEvent") {
		t.Fatalf("public reflection missing DeliverEvent")
	}
}

func TestPublicGRPCAgentCreateSessionFillsSubjectFields(t *testing.T) {
	t.Parallel()

	provider := newMemoryAgentProvider()
	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth(), func(cfg *server.Config) {
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: provider}
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	ctx := bearerContext(context.Background(), "valid-token")
	session, err := proto.NewAgentClient(conn).CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("Agent.CreateSession: %v", err)
	}
	got, err := proto.NewAgentClient(conn).GetSession(ctx, &proto.GetAgentProviderSessionRequest{
		ProviderName: "managed",
		SessionId:    session.GetId(),
	})
	if err != nil {
		t.Fatalf("Agent.GetSession: %v", err)
	}
	if got.GetCreatedBySubjectId() != "user:public" {
		t.Fatalf("created_by_subject_id = %q, want user:public", got.GetCreatedBySubjectId())
	}
}

func TestPublicGRPCAgentCreateSessionRejectsClientSubject(t *testing.T) {
	t.Parallel()

	conn, _ := startPublicGRPCTestServer(t, &relayTestInvoker{}, publicGRPCTestAuth(), func(cfg *server.Config) {
		agentControl := &stubAgentControl{defaultProviderName: "managed", provider: newMemoryAgentProvider()}
		cfg.Agent = agentControl
		cfg.AgentManager = agentmanager.New(agentmanager.Config{
			Agent:      agentControl,
			TurnScopes: newServerTestAgentTurnScopes(),
			ToolIDs:    newServerTestAgentToolIDs(t),
		})
	})
	ctx := bearerContext(context.Background(), "valid-token")
	_, err := proto.NewAgentClient(conn).CreateSession(ctx, &proto.CreateAgentProviderSessionRequest{
		ProviderName: "managed",
		Model:        "gpt-5.4",
		Subject:      &proto.SubjectContext{Id: "user:spoof"},
	})
	assertPublicGRPCCode(t, err, codes.InvalidArgument)
}

func publicGRPCTestAuth() *coretesting.StubAuthProvider {
	return &coretesting.StubAuthProvider{
		N: "public-grpc-test",
		IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			if req == nil || req.Token != "valid-token" {
				return &core.IntrospectResponse{Active: false}, nil
			}
			return &core.IntrospectResponse{
				Active:   true,
				Subject:  "user:public",
				ClientID: core.DefaultOAuthClientID,
			}, nil
		},
	}
}

func startPublicGRPCTestServer(
	t *testing.T,
	invoker *relayTestInvoker,
	auth *coretesting.StubAuthProvider,
	opts ...func(*server.Config),
) (*grpc.ClientConn, net.Listener) {
	t.Helper()
	handler, listener := newPublicGRPCTestHandler(t, invoker, auth, opts...)
	startPublicGRPCTestListener(t, handler, listener)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, listener
}

func newPublicGRPCTestHandler(
	t *testing.T,
	invoker *relayTestInvoker,
	auth *coretesting.StubAuthProvider,
	opts ...func(*server.Config),
) (http.Handler, net.Listener) {
	t.Helper()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetIdentityProvider(auth)
	transport.SetAuthorizationProvider(allowAllAuthorizationProvider{})
	transport.SetPublicMethodRegistry(registry)
	transport.SetPublicBaseURL("https://gestalt.example")

	cfg := server.Config{
		Auth:                 auth,
		Services:             testutil.NewStubServices(t),
		Providers:            testutil.NewProviderRegistry(t),
		StateSecret:          []byte("0123456789abcdef0123456789abcdef"),
		RouteProfile:         server.RouteProfilePublic,
		Invoker:              invoker,
		AppInvocation:        invoker,
		PublicGateway:        transport,
		RawAuthorization:     allowAllAuthorizationProvider{},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	handler, err := server.New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return handler, listener
}

func startPublicGRPCTestListener(t *testing.T, handler http.Handler, listener net.Listener) {
	t.Helper()
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	httpServer.Protocols = &http.Protocols{}
	httpServer.Protocols.SetHTTP1(true)
	httpServer.Protocols.SetUnencryptedHTTP2(true)

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
}

func bearerContext(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}

func assertPublicGRPCCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %v", want)
	}
	if grpcstatus.Code(err) != want {
		t.Fatalf("status.Code(err) = %v, want %v (%v)", grpcstatus.Code(err), want, err)
	}
}

type allowAllAuthorizationProvider struct{}

func (allowAllAuthorizationProvider) CheckAccess(context.Context, *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	return &proto.CheckAccessResponse{Allowed: true}, nil
}

func (allowAllAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{}, nil
}

func (allowAllAuthorizationProvider) SetAuthorizationState(context.Context, *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	return &proto.SetAuthorizationStateResponse{}, nil
}

func (allowAllAuthorizationProvider) ListRelationships(context.Context, *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	return &proto.ListRelationshipsResponse{}, nil
}

func (allowAllAuthorizationProvider) AddRelationship(context.Context, *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	return &proto.AddRelationshipResponse{}, nil
}

func (allowAllAuthorizationProvider) DeleteRelationship(context.Context, *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	return &proto.DeleteRelationshipResponse{}, nil
}

func (allowAllAuthorizationProvider) GetActiveModelRef(context.Context) (*proto.GetActiveModelRefResponse, error) {
	return &proto.GetActiveModelRefResponse{}, nil
}

func (allowAllAuthorizationProvider) SetActiveModel(context.Context, *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	return &proto.SetActiveModelResponse{}, nil
}

func (allowAllAuthorizationProvider) ListActiveModelResourceTypes(context.Context, *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	return &proto.ListActiveModelResourceTypesResponse{}, nil
}

func (allowAllAuthorizationProvider) Ping(context.Context) error { return nil }

func (allowAllAuthorizationProvider) Close() error { return nil }
