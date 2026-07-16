package server

import (
	"context"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	core "github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	identityservice "github.com/valon-technologies/gestalt/server/services/identity"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestPublicGRPCInterceptorForwardsCallerBearerToIdentity(t *testing.T) {
	t.Parallel()

	registry, err := publicrpc.NewGeneratedRegistry()
	if err != nil {
		t.Fatalf("NewGeneratedRegistry: %v", err)
	}
	var capturedCallerBearer string
	auth := &coretesting.StubAuthProvider{
		IntrospectFn: func(context.Context, *core.IntrospectRequest) (*core.IntrospectResponse, error) {
			return &core.IntrospectResponse{Active: true, Subject: "user:alice"}, nil
		},
		UserInfoFn: func(ctx context.Context, _ *core.UserInfoRequest) (*core.UserInfoResponse, error) {
			capturedCallerBearer = gestalt.CallerBearerTokenFromIncomingContext(ctx)
			if capturedCallerBearer == "" {
				capturedCallerBearer = gestalt.IdentityCallContextFromContext(ctx).CallerBearerToken
			}
			return &core.UserInfoResponse{
				SubjectID: "user:alice",
				Email:     "alice@example.com",
			}, nil
		},
	}
	transport := providergateway.NewProviderGatewayTransport()
	transport.SetPublicMethods(registry)
	transport.SetIdentityProvider(auth)

	srv := grpc.NewServer(grpc.UnaryInterceptor(publicPrepareUnaryInterceptor(transport)))
	publicrpc.RegisterPublicServers(srv, publicrpc.Servers{
		Identity: identityservice.NewProviderServer(auth),
	})
	conn, err := publicrpc.NewInProcessConn(srv)
	if err != nil {
		t.Fatalf("NewInProcessConn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	client := proto.NewIdentityClient(conn.ClientConn())
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer public-access-token",
	))

	if _, err := client.UserInfo(ctx, &proto.UserInfoRequest{}); err != nil {
		t.Fatalf("UserInfo: %v", err)
	}
	if capturedCallerBearer != "public-access-token" {
		t.Fatalf("UserInfo caller bearer = %q, want %q", capturedCallerBearer, "public-access-token")
	}
}
