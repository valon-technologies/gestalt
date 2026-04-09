package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type secretsServer struct {
	proto.UnimplementedSecretsProviderServer
	secrets SecretsProvider
}

func newSecretsProviderServer(secrets SecretsProvider) *secretsServer {
	return &secretsServer{secrets: secrets}
}

func (s *secretsServer) GetSecret(ctx context.Context, req *proto.GetSecretRequest) (*proto.GetSecretResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	value, err := s.secrets.GetSecret(ctx, req.GetName())
	if err != nil {
		return nil, providerRPCError("get secret", err)
	}
	return &proto.GetSecretResponse{Value: value}, nil
}
