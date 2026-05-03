package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type externalCredentialServer struct {
	proto.UnimplementedExternalCredentialProviderServer
	provider ExternalCredentialProvider
}

func newExternalCredentialProviderServer(provider ExternalCredentialProvider) *externalCredentialServer {
	return &externalCredentialServer{provider: provider}
}

func (s *externalCredentialServer) UpsertCredential(ctx context.Context, req *proto.UpsertExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	credential, err := s.provider.UpsertCredential(ctx, req)
	if err != nil {
		return nil, providerRPCError("upsert external credential", err)
	}
	if credential == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil credential")
	}
	return credential, nil
}

func (s *externalCredentialServer) GetCredential(ctx context.Context, req *proto.GetExternalCredentialRequest) (*proto.ExternalCredential, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	credential, err := s.provider.GetCredential(ctx, req)
	if err != nil {
		return nil, providerRPCError("get external credential", err)
	}
	if credential == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil credential")
	}
	return credential, nil
}

func (s *externalCredentialServer) ListCredentials(ctx context.Context, req *proto.ListExternalCredentialsRequest) (*proto.ListExternalCredentialsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListCredentials(ctx, req)
	if err != nil {
		return nil, providerRPCError("list external credentials", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil response")
	}
	return resp, nil
}

func (s *externalCredentialServer) DeleteCredential(ctx context.Context, req *proto.DeleteExternalCredentialRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.DeleteCredential(ctx, req); err != nil {
		return nil, providerRPCError("delete external credential", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *externalCredentialServer) ValidateCredentialConfig(ctx context.Context, req *proto.ValidateExternalCredentialConfigRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.ValidateCredentialConfig(ctx, req); err != nil {
		return nil, providerRPCError("validate external credential config", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *externalCredentialServer) ResolveCredential(ctx context.Context, req *proto.ResolveExternalCredentialRequest) (*proto.ResolveExternalCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ResolveCredential(ctx, req)
	if err != nil {
		return nil, providerRPCError("resolve external credential", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil response")
	}
	return resp, nil
}

func (s *externalCredentialServer) ExchangeCredential(ctx context.Context, req *proto.ExchangeExternalCredentialRequest) (*proto.ExchangeExternalCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ExchangeCredential(ctx, req)
	if err != nil {
		return nil, providerRPCError("exchange external credential", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "external credential provider returned nil response")
	}
	return resp, nil
}
