package runtimehost

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// bigPayloadEchoService is an ad-hoc gRPC service registered only for the
// regression test below. It echoes a bytes payload back to the caller so we
// can exercise both the client recv and client send paths of a host-side
// dial against an in-process server.
const (
	bigPayloadEchoServiceName = "test.runtimehost.BigPayloadEcho"
	bigPayloadEchoMethodName  = "Echo"
)

type bigPayloadEchoServer interface {
	Echo(ctx context.Context, req *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error)
}

type bigPayloadEchoImpl struct {
	response *wrapperspb.BytesValue
}

func (s *bigPayloadEchoImpl) Echo(_ context.Context, _ *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	return s.response, nil
}

func bigPayloadEchoHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(wrapperspb.BytesValue)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(bigPayloadEchoServer).Echo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/" + bigPayloadEchoServiceName + "/" + bigPayloadEchoMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(bigPayloadEchoServer).Echo(ctx, req.(*wrapperspb.BytesValue))
	}
	return interceptor(ctx, in, info, handler)
}

var bigPayloadEchoServiceDesc = grpc.ServiceDesc{
	ServiceName: bigPayloadEchoServiceName,
	HandlerType: (*bigPayloadEchoServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: bigPayloadEchoMethodName,
			Handler:    bigPayloadEchoHandler,
		},
	},
	Metadata: "test_big_payload_echo.proto",
}

// TestDialUnixSocketAllowsMessagesAboveDefaultLimit is a regression test for
// the host-side gRPC client cap. Before raising the cap, grpc-go's 4 MiB
// default MaxCallRecvMsgSize caused ResourceExhausted on the host whenever a
// provider returned a payload larger than 4 MiB (for example, the
// valon-profile provider's base64-encoded photos for some employees), which
// the provider surfaced as HTTP 502. The test asserts that an 8 MiB payload
// now round-trips cleanly through dialUnixSocket without ResourceExhausted on
// either direction.
func TestDialUnixSocketAllowsMessagesAboveDefaultLimit(t *testing.T) {
	t.Parallel()

	root, err := os.MkdirTemp("/tmp", "gstp-msg-size-test-")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socket := filepath.Join(root, "app.sock")

	lis, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}

	const payloadBytes = 8 * 1024 * 1024 // 8 MiB, well above the grpc-go 4 MiB default
	payload := make([]byte, payloadBytes)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	impl := &bigPayloadEchoImpl{response: &wrapperspb.BytesValue{Value: payload}}

	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(providerMaxGRPCMessageSize),
		grpc.MaxSendMsgSize(providerMaxGRPCMessageSize),
	)
	srv.RegisterService(&bigPayloadEchoServiceDesc, impl)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		<-serveErr
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := dialUnixSocket(ctx, socket, ProcessConfig{ProviderName: "runtimehost-message-size-test"})
	if err != nil {
		t.Fatalf("dialUnixSocket() error: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// 1) Client send path: the client should be allowed to send an 8 MiB request
	// without hitting its own MaxCallSendMsgSize cap.
	// 2) Client recv path: the client should be allowed to receive an 8 MiB
	// response without hitting its own MaxCallRecvMsgSize cap (this is the path
	// the production incident exercised).
	req := &wrapperspb.BytesValue{Value: payload}
	resp := new(wrapperspb.BytesValue)
	fullMethod := "/" + bigPayloadEchoServiceName + "/" + bigPayloadEchoMethodName
	if err := conn.Invoke(ctx, fullMethod, req, resp); err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.ResourceExhausted {
			t.Fatalf("dialUnixSocket() client returned ResourceExhausted for an %d-byte payload; regression on grpc-go 4 MiB default: %v", payloadBytes, err)
		}
		t.Fatalf("conn.Invoke(%s) error: %v", fullMethod, err)
	}
	if got := len(resp.GetValue()); got < payloadBytes {
		t.Fatalf("response payload length = %d, want >= %d", got, payloadBytes)
	}
}
