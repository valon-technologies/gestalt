package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type providerServer struct {
	proto.UnimplementedAuthorizationServer
	provider core.AuthorizationProvider
}

func NewProviderServer(provider core.AuthorizationProvider) proto.AuthorizationServer {
	return &providerServer{provider: provider}
}

func (s *providerServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccess(s.gatewayContext(ctx), req)
}

func (s *providerServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccessMany(s.gatewayContext(ctx), req)
}

func (s *providerServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListRelationships(s.gatewayContext(ctx), req)
}

func (s *providerServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.AddRelationship(s.gatewayContext(ctx), req)
}

func (s *providerServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.DeleteRelationship(s.gatewayContext(ctx), req)
}

func (s *providerServer) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.SetAuthorizationState(s.gatewayContext(ctx), req)
}

func (s *providerServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.GetActiveModelRef(s.gatewayContext(ctx))
}

func (s *providerServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.SetActiveModel(s.gatewayContext(ctx), req)
}

func (s *providerServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListActiveModelResourceTypes(s.gatewayContext(ctx), req)
}

func (s *providerServer) gatewayContext(ctx context.Context) context.Context {
	return invocation.WithEntry(ctx, invocation.EntryGRPC)
}

func (s *providerServer) requireProvider() error {
	if s == nil || s.provider == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is not configured")
	}
	return nil
}
