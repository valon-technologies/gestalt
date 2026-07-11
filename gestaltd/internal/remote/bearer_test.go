package remote

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestBearerTokenInterceptorsAttachMetadata(t *testing.T) {
	t.Parallel()

	unary, stream := bearerTokenInterceptors("secret-token")

	var unaryAuth []string
	err := unary(context.Background(), "/gestalt.provider.v1.App/Invoke", nil, nil, nil,
		func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
			md, _ := metadata.FromOutgoingContext(ctx)
			unaryAuth = md.Get("authorization")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unary interceptor: %v", err)
	}
	if len(unaryAuth) != 1 || unaryAuth[0] != "Bearer secret-token" {
		t.Fatalf("authorization metadata = %#v, want Bearer secret-token", unaryAuth)
	}

	var streamAuth []string
	_, err = stream(context.Background(), &grpc.StreamDesc{}, nil, "/gestalt.provider.v1.App/Invoke",
		func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			md, _ := metadata.FromOutgoingContext(ctx)
			streamAuth = md.Get("authorization")
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("stream interceptor: %v", err)
	}
	if len(streamAuth) != 1 || streamAuth[0] != "Bearer secret-token" {
		t.Fatalf("stream authorization metadata = %#v, want Bearer secret-token", streamAuth)
	}
}
