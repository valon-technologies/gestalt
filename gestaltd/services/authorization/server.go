package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type providerServer struct {
	proto.UnimplementedAuthorizationServer
	provider      core.AuthorizationProvider
	gatewaySource providergateway.GatewaySource
}

type ProviderServerOption func(*providerServer)

func WithGatewaySource(source providergateway.GatewaySource) ProviderServerOption {
	return func(s *providerServer) {
		s.gatewaySource = source
	}
}

func NewProviderServer(provider core.AuthorizationProvider, opts ...ProviderServerOption) proto.AuthorizationServer {
	s := &providerServer{provider: provider}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func (s *providerServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccess(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.CheckAccessMany(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListRelationships(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.AddRelationship(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.DeleteRelationship(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.SetAuthorizationState(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.GetActiveModelRef(s.providerGatewayContext(ctx))
}

func (s *providerServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.SetActiveModel(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	return s.provider.ListActiveModelResourceTypes(s.providerGatewayContext(ctx), req)
}

func (s *providerServer) providerGatewayContext(ctx context.Context) context.Context {
	if s == nil || s.gatewaySource == "" {
		return ctx
	}
	return providergateway.WithSource(ctx, s.gatewaySource)
}

func (s *providerServer) requireProvider() error {
	if s == nil || s.provider == nil {
		return status.Error(codes.FailedPrecondition, "authorization provider is not configured")
	}
	return nil
}
