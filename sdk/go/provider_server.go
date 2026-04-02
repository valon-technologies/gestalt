package gestalt

import (
	"context"
	"slices"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ConfigSchemaProvider is an optional interface a [Provider] can implement
// to declare a schema document for the provider-level configuration it accepts.
// The document is validated as JSON Schema and may be encoded as JSON or YAML.
type ConfigSchemaProvider interface {
	ConfigSchema() string
}

// ProviderServer adapts a [Provider] implementation to the gRPC
// ProviderPlugin service. Most plugin authors should use [ServeProvider]
// instead of constructing this directly.
type ProviderServer struct {
	proto.UnimplementedProviderPluginServer
	provider Provider
}

// NewProviderServer wraps a [Provider] in a [ProviderServer] ready to be
// registered on a gRPC server.
func NewProviderServer(provider Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) StartProvider(ctx context.Context, req *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if starter, ok := s.provider.(ProviderStarter); ok {
		if err := starter.Start(ctx, req.GetName(), mapFromStruct(req.GetConfig())); err != nil {
			return nil, status.Errorf(codes.Unknown, "start provider: %v", err)
		}
	}
	return &proto.StartProviderResponse{
		ProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	var connParams map[string]*proto.ConnectionParamDef
	if cpp, ok := s.provider.(ConnectionParamProvider); ok {
		connParams = connectionParamDefsToProto(cpp.ConnectionParamDefs())
	}
	staticCatalog, err := catalogToJSON(s.provider.Catalog())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode static catalog: %v", err)
	}
	var configSchema string
	if csp, ok := s.provider.(ConfigSchemaProvider); ok {
		configSchema = csp.ConfigSchema()
	}
	return &proto.ProviderMetadata{
		Name:                   s.provider.Name(),
		DisplayName:            s.provider.DisplayName(),
		Description:            s.provider.Description(),
		ConnectionMode:         coreConnectionModeToProto(s.provider.ConnectionMode()),
		ConnectionParams:       connParams,
		StaticCatalogJson:      staticCatalog,
		SupportsSessionCatalog: supportsSessionCatalog(s.provider),
		ConfigSchema:           configSchema,
		AuthTypes:              authTypes(s.provider),
	}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	if result == nil {
		return nil, status.Error(codes.Internal, "provider returned nil result")
	}
	return &proto.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	scp, ok := s.provider.(SessionCatalogProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	cat, err := scp.CatalogForRequest(ctx, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "session catalog: %v", err)
	}
	raw, err := catalogToJSON(cat)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode session catalog: %v", err)
	}
	return &proto.GetSessionCatalogResponse{CatalogJson: raw}, nil
}

func authTypes(p Provider) []string {
	if atl, ok := p.(AuthTypeLister); ok {
		return slices.Clone(atl.AuthTypes())
	}
	if mp, ok := p.(ManualAuthProvider); ok && mp.SupportsManualAuth() {
		return []string{"manual"}
	}
	return nil
}

func supportsSessionCatalog(p Provider) bool {
	_, ok := p.(SessionCatalogProvider)
	return ok
}
