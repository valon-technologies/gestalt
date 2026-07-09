package remote

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestBearerTokenInterceptorsAttachAuthorizationHeader(t *testing.T) {
	t.Parallel()

	unary, stream := bearerTokenInterceptors("secret-token")

	var outgoing metadata.MD
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		outgoing, _ = metadata.FromOutgoingContext(ctx)
		return nil
	}
	if err := unary(context.Background(), "/test.Service/Method", nil, nil, nil, invoker); err != nil {
		t.Fatalf("unary interceptor: %v", err)
	}
	if got := outgoing.Get("authorization"); len(got) != 1 || got[0] != "Bearer secret-token" {
		t.Fatalf("authorization metadata = %#v, want Bearer secret-token", got)
	}

	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		outgoing, _ = metadata.FromOutgoingContext(ctx)
		return nil, nil
	}
	if _, err := stream(context.Background(), &grpc.StreamDesc{}, nil, "/test.Service/Stream", streamer); err != nil {
		t.Fatalf("stream interceptor: %v", err)
	}
	if got := outgoing.Get("authorization"); len(got) != 1 || got[0] != "Bearer secret-token" {
		t.Fatalf("stream authorization metadata = %#v, want Bearer secret-token", got)
	}
}
