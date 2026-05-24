package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	authorizationCapabilityEffectiveSearchResources = "effective_search_resources"
	authorizationCapabilityEffectiveSearchSubjects  = "effective_search_subjects"
	authorizationCapabilityExpand                   = "expand"
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
	resp, err := s.provider.Evaluate(ctx, accessEvaluationRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("evaluate", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil decision")
	}
	out, err := protoAccessDecision(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid decision: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) EvaluateMany(ctx context.Context, req *proto.AccessEvaluationsRequest) (*proto.AccessEvaluationsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.EvaluateMany(ctx, accessEvaluationsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("evaluate many", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoAccessEvaluationsResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) SearchResources(ctx context.Context, req *proto.ResourceSearchRequest) (*proto.ResourceSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchResources(ctx, resourceSearchRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("search resources", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoResourceSearchResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) SearchSubjects(ctx context.Context, req *proto.SubjectSearchRequest) (*proto.SubjectSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchSubjects(ctx, subjectSearchRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("search subjects", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoSubjectSearchResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) EffectiveSearchResources(ctx context.Context, req *proto.ResourceSearchRequest) (*proto.ResourceSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.provider.(AuthorizationProviderEffectiveSearch)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authorization provider does not implement effective resource search")
	}
	resp, err := provider.EffectiveSearchResources(ctx, resourceSearchRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("effective search resources", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoResourceSearchResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) EffectiveSearchSubjects(ctx context.Context, req *proto.EffectiveSubjectSearchRequest) (*proto.EffectiveSubjectSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.provider.(AuthorizationProviderEffectiveSearch)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authorization provider does not implement effective subject search")
	}
	resp, err := provider.EffectiveSearchSubjects(ctx, effectiveSubjectSearchRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("effective search subjects", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoEffectiveSubjectSearchResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) SearchActions(ctx context.Context, req *proto.ActionSearchRequest) (*proto.ActionSearchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.SearchActions(ctx, actionSearchRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("search actions", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoActionSearchResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) Expand(ctx context.Context, req *proto.ExpandRequest) (*proto.ExpandResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, ok := s.provider.(AuthorizationProviderExpansion)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authorization provider does not implement relationship expansion")
	}
	resp, err := provider.Expand(ctx, expandRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("expand", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoExpandResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) GetMetadata(ctx context.Context, _ *emptypb.Empty) (*proto.AuthorizationMetadata, error) {
	resp, err := s.provider.GetMetadata(ctx)
	if err != nil {
		return nil, providerRPCError("get metadata", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil metadata")
	}
	out := protoAuthorizationMetadata(resp)
	out.Capabilities = appendMissingAuthorizationCapabilities(out.Capabilities, authorizationProviderCapabilities(s.provider)...)
	return out, nil
}

func (s *authorizationServer) ReadRelationships(ctx context.Context, req *proto.ReadRelationshipsRequest) (*proto.ReadRelationshipsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ReadRelationships(ctx, readRelationshipsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("read relationships", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	out, err := protoReadRelationshipsResponse(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "authorization provider returned invalid response: %v", err)
	}
	return out, nil
}

func (s *authorizationServer) WriteRelationships(ctx context.Context, req *proto.WriteRelationshipsRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.WriteRelationships(ctx, writeRelationshipsRequestFromProto(req)); err != nil {
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
	return protoGetActiveModelResponse(resp), nil
}

func (s *authorizationServer) ListModels(ctx context.Context, req *proto.ListModelsRequest) (*proto.ListModelsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.ListModels(ctx, listModelsRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("list models", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil response")
	}
	return protoListModelsResponse(resp), nil
}

func (s *authorizationServer) WriteModel(ctx context.Context, req *proto.WriteModelRequest) (*proto.AuthorizationModelRef, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.WriteModel(ctx, writeModelRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("write model", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authorization provider returned nil model")
	}
	return protoAuthorizationModelRef(resp), nil
}

func authorizationProviderCapabilities(provider AuthorizationProvider) []string {
	var capabilities []string
	if _, ok := provider.(AuthorizationProviderEffectiveSearch); ok {
		capabilities = append(capabilities, authorizationCapabilityEffectiveSearchResources, authorizationCapabilityEffectiveSearchSubjects)
	}
	if _, ok := provider.(AuthorizationProviderExpansion); ok {
		capabilities = append(capabilities, authorizationCapabilityExpand)
	}
	return capabilities
}

func appendMissingAuthorizationCapabilities(capabilities []string, required ...string) []string {
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
