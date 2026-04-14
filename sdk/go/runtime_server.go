package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
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
	if req.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"host requested protocol version %d, provider requires %d",
			req.GetProtocolVersion(),
			proto.CurrentProtocolVersion,
		)
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

func providerKindToProto(kind ProviderKind) proto.ProviderKind {
	switch kind {
	case ProviderKindIntegration:
		return proto.ProviderKind_PROVIDER_KIND_INTEGRATION
	case ProviderKindAuth:
		return proto.ProviderKind_PROVIDER_KIND_AUTH
	case ProviderKindIndexedDB:
		return proto.ProviderKind_PROVIDER_KIND_INDEXEDDB
	case ProviderKindCache:
		return proto.ProviderKind_PROVIDER_KIND_CACHE
	case ProviderKindSecrets:
		return proto.ProviderKind_PROVIDER_KIND_SECRETS
	case ProviderKindS3:
		return proto.ProviderKind_PROVIDER_KIND_S3
	default:
		return proto.ProviderKind_PROVIDER_KIND_UNSPECIFIED
	}
}
