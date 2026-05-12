package authorization

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const DefaultSocketEnv = "GESTALT_AUTHORIZATION_SOCKET"

const (
	capabilitySearchSubjects           = "search_subjects"
	capabilityEffectiveSearchResources = "effective_search_resources"
	capabilityEffectiveSearchSubjects  = "effective_search_subjects"
	capabilityExpand                   = "expand"
)

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
		return nil, authorizationProviderRPCError("search subjects", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) EffectiveSearchResources(ctx context.Context, req *proto.ResourceSearchRequest) (*proto.ResourceSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.provider.(core.AuthorizationProviderEffectiveSearch)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authorization provider does not implement effective resource search")
	}
	resp, err := provider.EffectiveSearchResources(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("effective search resources", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) EffectiveSearchSubjects(ctx context.Context, req *proto.EffectiveSubjectSearchRequest) (*proto.EffectiveSubjectSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.provider.(core.AuthorizationProviderEffectiveSearch)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authorization provider does not implement effective subject search")
	}
	resp, err := provider.EffectiveSearchSubjects(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("effective search subjects", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) Expand(ctx context.Context, req *proto.ExpandRequest) (*proto.ExpandResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.provider.(core.AuthorizationProviderExpansion)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authorization provider does not implement relationship expansion")
	}
	resp, err := provider.Expand(ctx, req)
	if err != nil {
		return nil, authorizationProviderRPCError("expand", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return resp, nil
}

func (s *authorizationProviderServer) GetMetadata(ctx context.Context, _ *emptypb.Empty) (*proto.AuthorizationMetadata, error) {
	providerMetadata, err := s.provider.GetMetadata(ctx)
	if err != nil {
		return nil, authorizationProviderRPCError("get metadata", err)
	}
	resp := &proto.AuthorizationMetadata{}
	if providerMetadata != nil {
		resp.Capabilities = append([]string(nil), providerMetadata.GetCapabilities()...)
		resp.ActiveModelId = providerMetadata.GetActiveModelId()
	}
	resp.Capabilities = appendMissingCapabilities(resp.Capabilities, authorizationHostCapabilities(s.provider)...)
	return resp, nil
}

func (s *authorizationProviderServer) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.WriteRelationships(ctx, req); err != nil {
		return nil, authorizationProviderRPCError("write relationships", err)
	}
	return &emptypb.Empty{}, nil
}

func authorizationHostCapabilities(provider core.AuthorizationProvider) []string {
	capabilities := []string{capabilitySearchSubjects}
	if _, ok := provider.(core.AuthorizationProviderEffectiveSearch); ok {
		capabilities = append(capabilities, capabilityEffectiveSearchResources, capabilityEffectiveSearchSubjects)
	}
	if _, ok := provider.(core.AuthorizationProviderExpansion); ok {
		capabilities = append(capabilities, capabilityExpand)
	}
	return capabilities
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

func appendMissingCapabilities(capabilities []string, required ...string) []string {
	seen := make(map[string]struct{}, len(capabilities)+len(required))
	for _, capability := range capabilities {
		seen[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := seen[capability]; ok {
			continue
		}
		capabilities = append(capabilities, capability)
		seen[capability] = struct{}{}
	}
	return capabilities
}
