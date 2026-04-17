package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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

func (s *authorizationServer) Evaluate(ctx context.Context, req *proto.AccessEvaluationRequest) (*proto.AccessDecision, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.Evaluate(ctx, req)
	if err != nil {
		return nil, providerRPCError("evaluate", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil decision")
	}
	return resp, nil
}

func (s *authorizationServer) EvaluateMany(ctx context.Context, req *proto.AccessEvaluationsRequest) (*proto.AccessEvaluationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.EvaluateMany(ctx, req)
	if err != nil {
		return nil, providerRPCError("evaluate many", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) SearchResources(ctx context.Context, req *proto.ResourceSearchRequest) (*proto.ResourceSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchResources(ctx, req)
	if err != nil {
		return nil, providerRPCError("search resources", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) SearchSubjects(ctx context.Context, req *proto.SubjectSearchRequest) (*proto.SubjectSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchSubjects(ctx, req)
	if err != nil {
		return nil, providerRPCError("search subjects", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) SearchActions(ctx context.Context, req *proto.ActionSearchRequest) (*proto.ActionSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchActions(ctx, req)
	if err != nil {
		return nil, providerRPCError("search actions", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) GetMetadata(ctx context.Context, _ *emptypb.Empty) (*proto.AuthorizationMetadata, error) {
	resp, err := s.provider.GetMetadata(ctx)
	if err != nil {
		return nil, providerRPCError("get metadata", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil metadata")
	}
	return resp, nil
}

func (s *authorizationServer) ReadRelationships(ctx context.Context, req *proto.ReadRelationshipsRequest) (*proto.ReadRelationshipsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ReadRelationships(ctx, req)
	if err != nil {
		return nil, providerRPCError("read relationships", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.WriteRelationships(ctx, req); err != nil {
		return nil, providerRPCError("write relationships", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *authorizationServer) GetActiveModel(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelResponse, error) {
	resp, err := s.provider.GetActiveModel(ctx)
	if err != nil {
		return nil, providerRPCError("get active model", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) ListModels(ctx context.Context, req *proto.ListModelsRequest) (*proto.ListModelsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListModels(ctx, req)
	if err != nil {
		return nil, providerRPCError("list models", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationServer) WriteModel(ctx context.Context, req *proto.WriteModelRequest) (*proto.AuthorizationModelRef, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.WriteModel(ctx, req)
	if err != nil {
		return nil, providerRPCError("write model", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil model")
	}
	return resp, nil
}
