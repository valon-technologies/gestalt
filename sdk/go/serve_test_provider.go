package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServeTestProvider starts a gRPC server for a [TestProvider].
func ServeTestProvider(ctx context.Context, provider TestProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindTest, provider))
		proto.RegisterTestProviderServer(srv, testProviderServer{provider: provider})
	})
}

type testProviderServer struct {
	proto.UnimplementedTestProviderServer
	provider TestProvider
}

func (s testProviderServer) HelloWorld(ctx context.Context, _ *proto.HelloWorldRequest) (*proto.HelloWorldResponse, error) {
	resp, err := s.provider.HelloWorld(ctx, &HelloWorldRequest{})
	if err != nil {
		return nil, providerRPCError("test hello world", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "test hello world returned nil response")
	}
	return &proto.HelloWorldResponse{Message: resp.Message}, nil
}
