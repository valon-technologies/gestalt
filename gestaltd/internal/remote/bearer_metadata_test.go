package remote

import (
	"context"
	"net"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

type capturingAppServer struct {
	proto.UnimplementedAppServer
	auth string
}

func (s *capturingAppServer) Invoke(ctx context.Context, _ *proto.AppInvokeRequest) (*proto.OperationResult, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if vals := md.Get("authorization"); len(vals) > 0 {
		s.auth = vals[0]
	}
	return &proto.OperationResult{Status: 200, Body: []byte("ok")}, nil
}

func TestBearerTokenInterceptorsAttachAuthorizationMetadata(t *testing.T) {
	t.Parallel()

	const wantToken = "gst_api_remote_dev_token"
	capture := &capturingAppServer{}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	proto.RegisterAppServer(srv, capture)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	unary, stream := bearerTokenInterceptors(wantToken)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(unary),
		grpc.WithChainStreamInterceptor(stream),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	_, err = proto.NewAppClient(conn).Invoke(context.Background(), &proto.AppInvokeRequest{
		App:       "linear",
		Operation: "issues.list",
	})
	if err != nil {
		t.Fatalf("App.Invoke: %v", err)
	}
	if got, want := capture.auth, "Bearer "+wantToken; got != want {
		t.Fatalf("authorization metadata = %q, want %q", got, want)
	}
}
