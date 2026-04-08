package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type RuntimeServer struct {
	proto.UnimplementedPluginRuntimeServer
	kind     proto.PluginKind
	provider RuntimeProvider
}

func NewRuntimeProviderServer(kind ProviderKind, provider RuntimeProvider) *RuntimeServer {
	return &RuntimeServer{
		kind:     providerKindToProto(kind),
		provider: provider,
	}
}

func (s *RuntimeServer) GetPluginMetadata(_ context.Context, _ *emptypb.Empty) (*proto.PluginMetadata, error) {
	meta := proto.PluginMetadata{
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
	if meta.Kind == proto.PluginKind_PLUGIN_KIND_UNSPECIFIED {
		meta.Kind = s.kind
	}
	return &meta, nil
}

func (s *RuntimeServer) ConfigurePlugin(ctx context.Context, req *proto.ConfigurePluginRequest) (*proto.ConfigurePluginResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetProtocolVersion() != proto.CurrentProtocolVersion {
		return nil, status.Errorf(
			codes.FailedPrecondition,
			"host requested protocol version %d, plugin requires %d",
			req.GetProtocolVersion(),
			proto.CurrentProtocolVersion,
		)
	}
	if err := s.provider.Configure(ctx, req.GetName(), configFromRequest(req.GetConfig())); err != nil {
		return nil, status.Errorf(codes.Unknown, "configure plugin: %v", err)
	}
	return &proto.ConfigurePluginResponse{ProtocolVersion: proto.CurrentProtocolVersion}, nil
}

func (s *RuntimeServer) HealthCheck(ctx context.Context, _ *emptypb.Empty) (*proto.HealthCheckResponse, error) {
	if checker, ok := s.provider.(HealthChecker); ok {
		if err := checker.HealthCheck(ctx); err != nil {
			return &proto.HealthCheckResponse{
				Ready:   false,
				Message: err.Error(),
			}, nil
		}
		return &proto.HealthCheckResponse{Ready: true}, nil
	}
	if s.kind == proto.PluginKind_PLUGIN_KIND_DATASTORE {
		return &proto.HealthCheckResponse{
			Ready:   false,
			Message: "datastore provider must implement HealthChecker",
		}, nil
	}
	return &proto.HealthCheckResponse{Ready: true}, nil
}

func providerKindToProto(kind ProviderKind) proto.PluginKind {
	switch kind {
	case ProviderKindIntegration:
		return proto.PluginKind_PLUGIN_KIND_INTEGRATION
	case ProviderKindAuth:
		return proto.PluginKind_PLUGIN_KIND_AUTH
	case ProviderKindDatastore:
		return proto.PluginKind_PLUGIN_KIND_DATASTORE
	case ProviderKindSecrets:
		return proto.PluginKind_PLUGIN_KIND_SECRETS
	case ProviderKindTelemetry:
		return proto.PluginKind_PLUGIN_KIND_TELEMETRY
	default:
		return proto.PluginKind_PLUGIN_KIND_UNSPECIFIED
	}
}
