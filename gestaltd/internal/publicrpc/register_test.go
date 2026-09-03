package publicrpc

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestRESTGatewayServicesMatchRegistrations(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{}
	for _, registration := range (Servers{}).registrations() {
		if registration.registerREST == nil {
			continue
		}
		want[registration.description.ServiceName] = struct{}{}
	}

	if len(want) != len(restGatewayServices) {
		t.Fatalf("restGatewayServices count = %d, want %d", len(restGatewayServices), len(want))
	}
	for service := range want {
		if _, ok := restGatewayServices[service]; !ok {
			t.Fatalf("restGatewayServices missing %q", service)
		}
	}
	for service := range restGatewayServices {
		if _, ok := want[service]; !ok {
			t.Fatalf("restGatewayServices has unexpected %q", service)
		}
	}
}

func TestRegisterRESTGatewayRegistersAppRoute(t *testing.T) {
	t.Parallel()

	app := &stubAppServer{}
	testRESTGatewayRoute(t, "/api/v2/app/example/operations/sync", Servers{App: app}, func(server *grpc.Server) {
		proto.RegisterAppServer(server, app)
	})
}

func TestRegisterRESTGatewayRegistersAuthorizationWriteRoute(t *testing.T) {
	t.Parallel()

	authorization := &stubAuthorizationServer{}
	testRESTGatewayRoute(t, "/api/v2/authorization/relationships:write", Servers{Authorization: authorization}, func(server *grpc.Server) {
		proto.RegisterAuthorizationServer(server, authorization)
	})
}

func testRESTGatewayRoute(t *testing.T, path string, servers Servers, register func(*grpc.Server)) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	register(grpcServer)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mux := runtime.NewServeMux()
	if err := RegisterRESTGateway(context.Background(), mux, conn, servers); err != nil {
		t.Fatalf("RegisterRESTGateway: %v", err)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resp, err := http.Post(server.URL+path, "application/json", http.NoBody)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	})
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, string(body))
	}
}

type stubAppServer struct {
	proto.UnimplementedAppServer
}

func (stubAppServer) Invoke(context.Context, *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	return &proto.OperationResult{Status: 200}, nil
}

type stubAuthorizationServer struct {
	proto.UnimplementedAuthorizationServer
}

func (stubAuthorizationServer) WriteRelationships(context.Context, *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	return &proto.WriteRelationshipsResponse{}, nil
}
