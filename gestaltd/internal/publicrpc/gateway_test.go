package publicrpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	gestaltproto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type stubPublicServers struct {
	gestaltproto.UnimplementedAppServer
	gestaltproto.UnimplementedAgentServer
	gestaltproto.UnimplementedWorkflowServer
	gestaltproto.UnimplementedIdentityServer
	gestaltproto.UnimplementedAuthorizationServer
}

func TestPublicGatewayRoutesMatchContract(t *testing.T) {
	t.Parallel()

	contract := loadSurfaceContract(t)
	servers := publicrpc.Servers{
		App:           &stubPublicServers{},
		Agent:         &stubPublicServers{},
		Workflow:      &stubPublicServers{},
		Identity:      &stubPublicServers{},
		Authorization: &stubPublicServers{},
	}
	srv := grpc.NewServer()
	publicrpc.RegisterPublicServers(srv, servers)
	conn, err := publicrpc.NewInProcessConn(srv)
	if err != nil {
		t.Fatalf("NewInProcessConn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	mux := runtime.NewServeMux()
	if err := publicrpc.RegisterRESTGateway(context.Background(), mux, conn.ClientConn(), servers); err != nil {
		t.Fatalf("RegisterRESTGateway: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})

	for _, svc := range contract.Services {
		for _, method := range svc.Methods {
			if method.HTTP == nil {
				continue
			}
			path := substitutePath(method.HTTP.Path)
			req := httptest.NewRequest(method.HTTP.Verb, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound && rec.Body.String() == "" {
				t.Fatalf("%s %s (%s): gateway returned 404", method.HTTP.Verb, path, method.FullMethod)
			}
			if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), "path was not found") {
				t.Fatalf("%s %s (%s): route not registered", method.HTTP.Verb, path, method.FullMethod)
			}
		}
	}
}

func TestPublicGatewayRejectsWrongVerb(t *testing.T) {
	t.Parallel()

	servers := publicrpc.Servers{App: &stubPublicServers{}}
	srv := grpc.NewServer()
	publicrpc.RegisterPublicServers(srv, servers)
	conn, err := publicrpc.NewInProcessConn(srv)
	if err != nil {
		t.Fatalf("NewInProcessConn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	mux := runtime.NewServeMux()
	if err := publicrpc.RegisterRESTGateway(context.Background(), mux, conn.ClientConn(), servers); err != nil {
		t.Fatalf("RegisterRESTGateway: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v2/app/test-app/operations/test-operation", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("wrong verb status = %d, want non-success", rec.Code)
	}
}

func (stubPublicServers) Invoke(context.Context, *gestaltproto.AppInvokeRequest) (*gestaltproto.OperationResult, error) {
	return nil, status.Error(codes.Unauthenticated, "stub")
}

func (stubPublicServers) InvokeGraphQL(context.Context, *gestaltproto.AppInvokeGraphQLRequest) (*gestaltproto.OperationResult, error) {
	return nil, status.Error(codes.Unauthenticated, "stub")
}
