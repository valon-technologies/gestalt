package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authorizationServer struct {
	proto.UnimplementedAuthorizationProviderServer
	provider AuthorizationProvider
}

func newAuthorizationProviderServer(provider AuthorizationProvider) *authorizationServer {
	return &authorizationServer{provider: provider}
}

func (s *authorizationServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.CheckAccess(ctx, checkAccessRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("check access", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return protoCheckAccessResponse(resp), nil
}

func (s *authorizationServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.CheckAccessMany(ctx, checkAccessManyRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("check access many", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return protoCheckAccessManyResponse(resp), nil
}

func (s *authorizationServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListRelationships(ctx, listRelationshipsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("list relationships", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoListRelationshipsResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.AddRelationship(ctx, addRelationshipRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("add relationship", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoAddRelationshipResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if _, err := s.provider.DeleteRelationship(ctx, deleteRelationshipRequestFromProto(req)); err != nil {
		return nil, providerRPCError("delete relationship", err)
	}
	return &proto.DeleteRelationshipResponse{}, nil
}

func (s *authorizationServer) SetRelationships(ctx context.Context, req *proto.SetRelationshipsRequest) (*proto.SetRelationshipsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SetRelationships(ctx, setRelationshipsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("set relationships", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoSetRelationshipsResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	resp, err := s.provider.GetActiveModelRef(ctx)
	if err != nil {
		return nil, providerRPCError("get active model ref", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return protoGetActiveModelRefResponse(resp), nil
}

func (s *authorizationServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SetActiveModel(ctx, setActiveModelRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("set active model", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return protoSetActiveModelResponse(resp), nil
}

func (s *authorizationServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListActiveModelResourceTypes(ctx, listActiveModelResourceTypesRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("list active model resource types", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return protoListActiveModelResourceTypesResponse(resp), nil
}
