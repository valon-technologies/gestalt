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

	const invokePath = "/api/v2/app/example/operations/sync"

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	proto.RegisterAppServer(grpcServer, &stubAppServer{})
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
	if err := RegisterRESTGateway(context.Background(), mux, conn, Servers{
		App: &stubAppServer{},
	}); err != nil {
		t.Fatalf("RegisterRESTGateway: %v", err)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resp, err := http.Post(server.URL+invokePath, "application/json", http.NoBody)
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
