package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type runtimeServer struct {
	proto.UnimplementedProviderLifecycleServer
	kind     proto.ProviderKind
	provider Provider
}

func newRuntimeServer(kind ProviderKind, provider Provider) *runtimeServer {
	return &runtimeServer{
		kind:     providerKindToProto(kind),
		provider: provider,
	}
}

func (s *runtimeServer) GetProviderIdentity(_ context.Context, _ *emptypb.Empty) (*proto.ProviderIdentity, error) {
	meta := proto.ProviderIdentity{
		Kind:               s.kind,
		MinProtocolVersion: proto.CurrentProtocolVersion,
		MaxProtocolVersion: proto.CurrentProtocolVersion,
	}
	if provider, ok := s.provider.(MetadataProvider); ok {
		providerMeta := provider.Metadata()
		meta.Kind = providerKindToProto(providerMeta.Kind)
		meta.Name = providerMeta.Name
		meta.DisplayName = providerMeta.DisplayName
		meta.Description = providerMeta.Description
		meta.Version = providerMeta.Version
	}
	if provider, ok := s.provider.(WarningsProvider); ok {
		meta.Warnings = append(meta.Warnings, provider.Warnings()...)
	}
	if meta.Kind == proto.ProviderKind_PROVIDER_KIND_UNSPECIFIED {
		meta.Kind = s.kind
	}
	return &meta, nil
}

func (s *runtimeServer) ConfigureProvider(ctx context.Context, req *proto.ConfigureProviderRequest) (*proto.ConfigureProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := validateProtocolVersion(req.GetProtocolVersion()); err != nil {
		return nil, err
	}
	config := req.GetConfig().AsMap()
	if config == nil {
		config = map[string]any{}
	}
	if err := s.provider.Configure(ctx, req.GetName(), config); err != nil {
		return nil, status.Errorf(codes.Unknown, "configure provider: %v", err)
	}
	return &proto.ConfigureProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *runtimeServer) HealthCheck(ctx context.Context, _ *emptypb.Empty) (*proto.HealthCheckResponse, error) {
	if checker, ok := s.provider.(HealthChecker); ok {
		if err := checker.HealthCheck(ctx); err != nil {
			return &proto.HealthCheckResponse{
				Ready:   false,
				Message: err.Error(),
			}, nil
		}
		return &proto.HealthCheckResponse{Ready: true}, nil
	}
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func (s *runtimeServer) StartProvider(ctx context.Context, _ *emptypb.Empty) (*proto.StartRuntimeProviderResponse, error) {
	if starter, ok := s.provider.(Starter); ok {
		if err := starter.Start(ctx); err != nil {
			return nil, status.Errorf(codes.Unknown, "start provider: %v", err)
		}
	}
	return &proto.StartRuntimeProviderResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func providerKindToProto(kind ProviderKind) proto.ProviderKind {
	switch kind {
	case ProviderKindIntegration:
		return proto.ProviderKind_PROVIDER_KIND_INTEGRATION
	case ProviderKindAuthentication:
		return proto.ProviderKind_PROVIDER_KIND_AUTHENTICATION
	case ProviderKindAuthorization:
		return proto.ProviderKind_PROVIDER_KIND_AUTHORIZATION
	case ProviderKindIndexedDB:
		return proto.ProviderKind_PROVIDER_KIND_INDEXEDDB
	case ProviderKindCache:
		return proto.ProviderKind_PROVIDER_KIND_CACHE
	case ProviderKindSecrets:
		return proto.ProviderKind_PROVIDER_KIND_SECRETS
	case ProviderKindS3:
		return proto.ProviderKind_PROVIDER_KIND_S3
	case ProviderKindWorkflow:
		return proto.ProviderKind_PROVIDER_KIND_WORKFLOW
	case ProviderKindAgent:
		return proto.ProviderKind_PROVIDER_KIND_AGENT
	case ProviderKindExternalCredential:
		return proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL
	case ProviderKindRuntime:
		return proto.ProviderKind_PROVIDER_KIND_RUNTIME
	default:
		return proto.ProviderKind_PROVIDER_KIND_UNSPECIFIED
	}
}
