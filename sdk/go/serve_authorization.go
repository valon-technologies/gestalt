package gestalt

import (
	"context"

	authz "github.com/valon-technologies/gestalt/sdk/go/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ServeAuthorizationProvider starts a gRPC server for an
// [AuthorizationProvider].
func ServeAuthorizationProvider(ctx context.Context, provider AuthorizationProvider) error {
	return serveProvider(withProviderCloser(ctx, provider), func(srv *grpc.Server) {
		proto.RegisterProviderLifecycleServer(srv, newRuntimeServer(ProviderKindAuthorization, provider))
		proto.RegisterAuthorizationProviderServer(srv, authorizationProviderServer{provider: provider})
	})
}

type authorizationProviderServer struct {
	proto.UnimplementedAuthorizationProviderServer
	provider AuthorizationProvider
}

func (s authorizationProviderServer) CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	resp, err := s.provider.CheckAccess(ctx, authz.CheckAccessRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.CheckAccessResponseToProto, "authorization check access", err)
}

func (s authorizationProviderServer) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	resp, err := s.provider.CheckAccessMany(ctx, authz.CheckAccessManyRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.CheckAccessManyResponseToProto, "authorization check access many", err)
}

func (s authorizationProviderServer) ListRelationships(ctx context.Context, req *proto.ListRelationshipsRequest) (*proto.ListRelationshipsResponse, error) {
	resp, err := s.provider.ListRelationships(ctx, authz.ListRelationshipsRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.ListRelationshipsResponseToProto, "authorization list relationships", err)
}

func (s authorizationProviderServer) AddRelationship(ctx context.Context, req *proto.AddRelationshipRequest) (*proto.AddRelationshipResponse, error) {
	resp, err := s.provider.AddRelationship(ctx, authz.AddRelationshipRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.AddRelationshipResponseToProto, "authorization add relationship", err)
}

func (s authorizationProviderServer) DeleteRelationship(ctx context.Context, req *proto.DeleteRelationshipRequest) (*proto.DeleteRelationshipResponse, error) {
	resp, err := s.provider.DeleteRelationship(ctx, authz.DeleteRelationshipRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.DeleteRelationshipResponseToProto, "authorization delete relationship", err)
}

func (s authorizationProviderServer) SetAuthorizationState(ctx context.Context, req *proto.SetAuthorizationStateRequest) (*proto.SetAuthorizationStateResponse, error) {
	resp, err := s.provider.SetAuthorizationState(ctx, authz.SetAuthorizationStateRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.SetAuthorizationStateResponseToProto, "authorization set authorization state", err)
}

func (s authorizationProviderServer) GetActiveModelRef(ctx context.Context, _ *emptypb.Empty) (*proto.GetActiveModelRefResponse, error) {
	resp, err := s.provider.GetActiveModelRef(ctx)
	return authorizationProtoResponseErr(resp, authz.GetActiveModelRefResponseToProto, "authorization get active model ref", err)
}

func (s authorizationProviderServer) SetActiveModel(ctx context.Context, req *proto.SetActiveModelRequest) (*proto.SetActiveModelResponse, error) {
	resp, err := s.provider.SetActiveModel(ctx, authz.SetActiveModelRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.SetActiveModelResponseToProto, "authorization set active model", err)
}

func (s authorizationProviderServer) ListActiveModelResourceTypes(ctx context.Context, req *proto.ListActiveModelResourceTypesRequest) (*proto.ListActiveModelResourceTypesResponse, error) {
	resp, err := s.provider.ListActiveModelResourceTypes(ctx, authz.ListActiveModelResourceTypesRequestFromProto(req))
	return authorizationProtoResponseErr(resp, authz.ListActiveModelResourceTypesResponseToProto, "authorization list active model resource types", err)
}

func authorizationProtoResponse[SDK any, PB any](resp *SDK, convert func(*SDK) *PB, operation string, err error) (*PB, error) {
	if err != nil {
		return nil, providerRPCError(operation, err)
	}
	if resp == nil {
		return nil, status.Errorf(codes.Internal, "%s returned nil response", operation)
	}
	out := convert(resp)
	if out == nil {
		return nil, status.Errorf(codes.Internal, "%s returned nil response", operation)
	}
	return out, nil
}

func authorizationProtoResponseErr[SDK any, PB any](resp *SDK, convert func(*SDK) (*PB, error), operation string, err error) (*PB, error) {
	if err != nil {
		return nil, providerRPCError(operation, err)
	}
	if resp == nil {
		return nil, status.Errorf(codes.Internal, "%s returned nil response", operation)
	}
	out, err := convert(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%s returned invalid response: %v", operation, err)
	}
	if out == nil {
		return nil, status.Errorf(codes.Internal, "%s returned nil response", operation)
	}
	return out, nil
}
