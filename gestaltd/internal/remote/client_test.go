package remote_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestNewClientSetRequiresURLAndToken(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{})
	if err == nil || err.Error() != "remote URL is required" {
		t.Fatalf("NewClientSet() error = %v, want remote URL is required", err)
	}

	_, err = remote.NewClientSet(context.Background(), remote.Config{URL: "http://gestalt.example"})
	if err == nil || err.Error() != "remote token is required" {
		t.Fatalf("NewClientSet() error = %v, want remote token is required", err)
	}
}

func TestWithBearerAttachesAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	ctx := remote.WithBearer(context.Background(), "gst_api_test")
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("expected outgoing metadata")
	}
	values := md.Get("authorization")
	if len(values) != 1 || values[0] != "Bearer gst_api_test" {
		t.Fatalf("authorization metadata = %#v, want Bearer gst_api_test", values)
	}
}

func TestNewClientSetAttachesBearerAndPreservesStatus(t *testing.T) {
	t.Parallel()

	baseURL, appServer, closeServer := startMetadataTestServer(t)
	defer closeServer()

	clientSet, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   baseURL,
		Token: "gst_api_test",
	})
	if err != nil {
		t.Fatalf("NewClientSet: %v", err)
	}
	defer func() { _ = clientSet.Close() }()

	_, err = clientSet.App.Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Invoke status = %v, want PermissionDenied (%v)", status.Code(err), err)
	}
	if appServer.lastAuth != "Bearer gst_api_test" {
		t.Fatalf("lastAuth = %q, want Bearer gst_api_test", appServer.lastAuth)
	}
}

func TestNewClientSetRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	_, err := remote.NewClientSet(context.Background(), remote.Config{
		URL:   "ftp://gestalt.example",
		Token: "token",
	})
	if err == nil || !contains(err.Error(), "scheme") {
		t.Fatalf("NewClientSet() error = %v, want unsupported scheme", err)
	}
}

type metadataAppServer struct {
	proto.UnimplementedAppServer
	lastAuth string
}

func (s *metadataAppServer) Invoke(ctx context.Context, _ *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			s.lastAuth = values[0]
		}
	}
	return nil, status.Error(codes.PermissionDenied, "denied")
}

func startMetadataTestServer(t *testing.T) (baseURL string, appServer *metadataAppServer, closeFn func()) {
	t.Helper()

	appServer = &metadataAppServer{}
	server := grpc.NewServer()
	publicrpc.RegisterPublicAppServer(server, appServer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()

	return "http://" + listener.Addr().String(), appServer, func() {
		server.GracefulStop()
		if err := <-serverDone; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Fatalf("Serve: %v", err)
		}
		_ = listener.Close()
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexSubstring(s, substr) >= 0)
}

func indexSubstring(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

var _ = insecure.NewCredentials
