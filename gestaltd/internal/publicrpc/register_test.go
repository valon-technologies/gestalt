package publicrpc_test

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type originAppServer struct {
	gestaltproto.UnimplementedAppServer
	lastMethod string
}

func (s *originAppServer) Invoke(ctx context.Context, _ *gestaltproto.AppInvokeRequest) (*gestaltproto.OperationResult, error) {
	if origin, ok := publicrpc.PublicOriginFromContext(ctx); ok {
		s.lastMethod = origin.FullMethod
	}
	return &gestaltproto.OperationResult{}, nil
}

func (s *originAppServer) InvokeGraphQL(ctx context.Context, _ *gestaltproto.AppInvokeGraphQLRequest) (*gestaltproto.OperationResult, error) {
	if origin, ok := publicrpc.PublicOriginFromContext(ctx); ok {
		s.lastMethod = origin.FullMethod
	}
	return &gestaltproto.OperationResult{}, nil
}

func TestPublicRegistrationMarksOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fullMethod string
		call       func(gestaltproto.AppClient) error
	}{
		{
			name:       "Invoke",
			fullMethod: gestaltproto.App_Invoke_FullMethodName,
			call: func(client gestaltproto.AppClient) error {
				_, err := client.Invoke(context.Background(), &gestaltproto.AppInvokeRequest{})
				return err
			},
		},
		{
			name:       "InvokeGraphQL",
			fullMethod: gestaltproto.App_InvokeGraphQL_FullMethodName,
			call: func(client gestaltproto.AppClient) error {
				_, err := client.InvokeGraphQL(context.Background(), &gestaltproto.AppInvokeGraphQLRequest{})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serverImpl := &originAppServer{}
			conn := dialGRPC(t, func(s *grpc.Server) {
				publicrpc.RegisterPublicAppServer(s, serverImpl)
			})

			if err := tc.call(gestaltproto.NewAppClient(conn)); err != nil {
				t.Fatalf("call: %v", err)
			}
			if got, want := serverImpl.lastMethod, tc.fullMethod; got != want {
				t.Fatalf("FullMethod = %q, want %q", got, want)
			}
		})
	}
}

func TestPublicRegistrationExcludesInternalMethods(t *testing.T) {
	t.Parallel()

	tracker := &workflowTracker{}
	conn := dialGRPC(t, func(s *grpc.Server) {
		publicrpc.RegisterPublicWorkflowServer(s, tracker)
	})

	client := gestaltproto.NewWorkflowClient(conn)
	if _, err := client.DeliverEvent(context.Background(), &gestaltproto.DeliverWorkflowProviderEventRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("DeliverEvent code = %v, want Unimplemented", status.Code(err))
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

type workflowTracker struct {
	gestaltproto.UnimplementedWorkflowServer
	deliverCalled bool
	applyCalled   bool
}

func (t *workflowTracker) DeliverEvent(context.Context, *gestaltproto.DeliverWorkflowProviderEventRequest) (*gestaltproto.WorkflowEvent, error) {
	t.deliverCalled = true
	return nil, status.Error(codes.Unimplemented, "deliver")
}

func (t *workflowTracker) ApplyDefinition(context.Context, *gestaltproto.ApplyWorkflowProviderDefinitionRequest) (*gestaltproto.WorkflowDefinition, error) {
	t.applyCalled = true
	return nil, status.Error(codes.Unimplemented, "apply")
}

func dialGRPC(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	server := grpc.NewServer()
	register(server)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() {
		server.Stop()
		_ = lis.Close()
	})

	go func() { _ = server.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}
