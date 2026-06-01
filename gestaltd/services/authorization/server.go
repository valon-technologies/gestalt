package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
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

func (s *authorizationProviderServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.CheckAccess(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("check access", err)
	}
	return requireAuthorizationResponse(resp)
}

func (s *authorizationProviderServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.CheckAccessMany(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("check access many", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListRelationships(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("list relationships", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.AddRelationship(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("add relationship", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.DeleteRelationship(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("delete relationship", err)
	}
	if resp == nil {
		resp = &proto.DeleteRelationshipResponse{}
	}
	return resp, nil
}

func (s *authorizationProviderServer) SetRelationships(ctx context.Context, req *proto.SetRelationshipsRequest) (*proto.SetRelationshipsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SetRelationships(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("set relationships", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	resp, err := s.provider.GetActiveModelRef(ctx)
	if err != nil {
		return nil, authorizationProviderRPCError("get active model ref", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SetActiveModel(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("set active model", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListActiveModelResourceTypes(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("list active model resource types", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func requireAuthorizationResponse(resp *proto.CheckAccessResponse) (*proto.CheckAccessResponse, error) {
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func authorizationProviderRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok {
		return st.Err()
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}
