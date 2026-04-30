package authorization

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const DefaultSocketEnv = "GESTALT_AUTHORIZATION_SOCKET"

func SocketTokenEnv() string {
	return DefaultSocketEnv + "_TOKEN"
}

type authorizationProviderServer struct {
	proto.UnimplementedAuthorizationProviderServer
	provider core.AuthorizationProvider
}

func NewProviderServer(provider core.AuthorizationProvider) proto.AuthorizationProviderServer {
	return &authorizationProviderServer{provider: provider}
}

func (s *authorizationProviderServer) SearchSubjects(ctx context.Context, req *proto.SubjectSearchRequest) (*proto.SubjectSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchSubjects(ctx, req)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "search subjects: %v", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) GetMetadata(context.Context, *emptypb.Empty) (*proto.AuthorizationMetadata, error) {
	return &proto.AuthorizationMetadata{
		Capabilities: []string{"search_subjects"},
	}, nil
}
