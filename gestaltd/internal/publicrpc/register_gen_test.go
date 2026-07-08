package publicrpc_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
)

type recordingAppServer struct {
	gestaltproto.UnimplementedAppServer
	invokeCalls        int
	invokeGraphQLCalls int
	lastOrigin         publicrpc.PublicOrigin
	sawOrigin          bool
}

func (s *recordingAppServer) Invoke(ctx context.Context, req *gestaltproto.AppInvokeRequest) (*gestaltproto.OperationResult, error) {
	s.invokeCalls++
	if origin, ok := publicrpc.PublicOriginFromContext(ctx); ok {
		s.lastOrigin = origin
		s.sawOrigin = true
	}
	_ = req
	return &gestaltproto.OperationResult{}, nil
}

func (s *recordingAppServer) InvokeGraphQL(ctx context.Context, req *gestaltproto.AppInvokeGraphQLRequest) (*gestaltproto.OperationResult, error) {
	s.invokeGraphQLCalls++
	if origin, ok := publicrpc.PublicOriginFromContext(ctx); ok {
		s.lastOrigin = origin
		s.sawOrigin = true
	}
	_ = req
	return &gestaltproto.OperationResult{}, nil
}

func TestPublicAppInvokeMarksContextAndDispatches(t *testing.T) {
	t.Parallel()

	serverImpl := &recordingAppServer{}
	grpcServer := grpc.NewServer()
	publicrpc.RegisterPublicAppServer(grpcServer, serverImpl)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer lis.Close()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer conn.Close()

	client := gestaltproto.NewAppClient(conn)
	if _, err := client.Invoke(context.Background(), &gestaltproto.AppInvokeRequest{}); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if serverImpl.invokeCalls != 1 {
		t.Fatalf("invokeCalls = %d, want 1", serverImpl.invokeCalls)
	}
	if !serverImpl.sawOrigin {
		t.Fatal("Invoke did not see public origin")
	}
	if got, want := serverImpl.lastOrigin.FullMethod, gestaltproto.App_Invoke_FullMethodName; got != want {
		t.Fatalf("FullMethod = %q, want %q", got, want)
	}
}

func TestPublicAppInvokeGraphQLMarksContextAndDispatches(t *testing.T) {
	t.Parallel()

	serverImpl := &recordingAppServer{}
	grpcServer := grpc.NewServer()
	publicrpc.RegisterPublicAppServer(grpcServer, serverImpl)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer lis.Close()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer conn.Close()

	client := gestaltproto.NewAppClient(conn)
	if _, err := client.InvokeGraphQL(context.Background(), &gestaltproto.AppInvokeGraphQLRequest{}); err != nil {
		t.Fatalf("InvokeGraphQL: %v", err)
	}
	if serverImpl.invokeGraphQLCalls != 1 {
		t.Fatalf("invokeGraphQLCalls = %d, want 1", serverImpl.invokeGraphQLCalls)
	}
	if !serverImpl.sawOrigin {
		t.Fatal("InvokeGraphQL did not see public origin")
	}
	if got, want := serverImpl.lastOrigin.FullMethod, gestaltproto.App_InvokeGraphQL_FullMethodName; got != want {
		t.Fatalf("FullMethod = %q, want %q", got, want)
	}
}

func TestPublicWorkflowRegistrationScope(t *testing.T) {
	t.Parallel()

	tracker := &workflowTracker{}
	conn := dialPublicServer(t, func(server *grpc.Server) {
		publicrpc.RegisterPublicWorkflowServer(server, tracker)
	})
	defer conn.Close()

	client := gestaltproto.NewWorkflowClient(conn)
	if _, err := client.DeliverEvent(context.Background(), &gestaltproto.DeliverWorkflowProviderEventRequest{}); !isUnimplemented(err) {
		t.Fatalf("DeliverEvent err = %v, want Unimplemented", err)
	}
	if !tracker.deliverCalled {
		t.Fatal("DeliverEvent did not reach public handler")
	}

	if _, err := client.ApplyDefinition(context.Background(), &gestaltproto.ApplyWorkflowProviderDefinitionRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("ApplyDefinition code = %v, want Unimplemented", status.Code(err))
	}
	if tracker.applyCalled {
		t.Fatal("ApplyDefinition reached server, want unregistered method")
	}
}

func TestPublicAgentRegistrationScope(t *testing.T) {
	t.Parallel()

	tracker := &agentTracker{}
	conn := dialPublicServer(t, func(server *grpc.Server) {
		publicrpc.RegisterPublicAgentServer(server, tracker)
	})
	defer conn.Close()

	client := gestaltproto.NewAgentClient(conn)
	for _, method := range []string{
		"CreateSession",
		"CreateTurn",
		"ListTurnEvents",
	} {
		if err := callAgentMethod(client, method); !isUnimplemented(err) {
			t.Fatalf("%s err = %v, want Unimplemented", method, err)
		}
		if !tracker.called[method] {
			t.Fatalf("%s did not reach public handler", method)
		}
	}

	for _, method := range []string{
		"GetInteraction",
		"GetCapabilities",
	} {
		if err := callAgentMethod(client, method); status.Code(err) != codes.Unimplemented {
			t.Fatalf("%s code = %v, want Unimplemented", method, status.Code(err))
		}
		if tracker.called[method] {
			t.Fatalf("%s reached server, want unregistered method", method)
		}
	}
}

type workflowTracker struct {
	gestaltproto.UnimplementedWorkflowServer
	deliverCalled bool
	applyCalled   bool
}

func (t *workflowTracker) DeliverEvent(ctx context.Context, req *gestaltproto.DeliverWorkflowProviderEventRequest) (*gestaltproto.WorkflowEvent, error) {
	t.deliverCalled = true
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "deliver")
}

func (t *workflowTracker) ApplyDefinition(ctx context.Context, req *gestaltproto.ApplyWorkflowProviderDefinitionRequest) (*gestaltproto.WorkflowDefinition, error) {
	t.applyCalled = true
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "apply")
}

type agentTracker struct {
	gestaltproto.UnimplementedAgentServer
	called map[string]bool
}

func (t *agentTracker) mark(method string) {
	if t.called == nil {
		t.called = map[string]bool{}
	}
	t.called[method] = true
}

func (t *agentTracker) CreateSession(ctx context.Context, req *gestaltproto.CreateAgentProviderSessionRequest) (*gestaltproto.AgentSession, error) {
	t.mark("CreateSession")
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "create session")
}

func (t *agentTracker) CreateTurn(ctx context.Context, req *gestaltproto.CreateAgentProviderTurnRequest) (*gestaltproto.AgentTurn, error) {
	t.mark("CreateTurn")
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "create turn")
}

func (t *agentTracker) ListTurnEvents(ctx context.Context, req *gestaltproto.ListAgentProviderTurnEventsRequest) (*gestaltproto.ListAgentProviderTurnEventsResponse, error) {
	t.mark("ListTurnEvents")
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "list turn events")
}

func (t *agentTracker) GetInteraction(ctx context.Context, req *gestaltproto.GetAgentProviderInteractionRequest) (*gestaltproto.AgentInteraction, error) {
	t.mark("GetInteraction")
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "get interaction")
}

func (t *agentTracker) GetCapabilities(ctx context.Context, req *gestaltproto.GetAgentProviderCapabilitiesRequest) (*gestaltproto.AgentProviderCapabilities, error) {
	t.mark("GetCapabilities")
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "get capabilities")
}

func callAgentMethod(client gestaltproto.AgentClient, method string) error {
	ctx := context.Background()
	switch method {
	case "CreateSession":
		_, err := client.CreateSession(ctx, &gestaltproto.CreateAgentProviderSessionRequest{})
		return err
	case "CreateTurn":
		_, err := client.CreateTurn(ctx, &gestaltproto.CreateAgentProviderTurnRequest{})
		return err
	case "ListTurnEvents":
		_, err := client.ListTurnEvents(ctx, &gestaltproto.ListAgentProviderTurnEventsRequest{})
		return err
	case "GetInteraction":
		_, err := client.GetInteraction(ctx, &gestaltproto.GetAgentProviderInteractionRequest{})
		return err
	case "GetCapabilities":
		_, err := client.GetCapabilities(ctx, &gestaltproto.GetAgentProviderCapabilitiesRequest{})
		return err
	default:
		return status.Error(codes.Internal, "unknown method")
	}
}

func dialPublicServer(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	grpcServer := grpc.NewServer()
	register(grpcServer)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = lis.Close()
	})

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return conn
}

func isUnimplemented(err error) bool {
	return status.Code(err) == codes.Unimplemented
}
