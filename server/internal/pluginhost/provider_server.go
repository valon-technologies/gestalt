package pluginhost

import (
	"context"
	"slices"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	proto.UnimplementedProviderPluginServer
	provider core.Provider
}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	var defs map[string]core.ConnectionParamDef
	if cpp, ok := s.provider.(core.ConnectionParamProvider); ok {
		defs = cpp.ConnectionParamDefs()
	}

	staticCatalog, err := catalogToJSON(staticCatalogForProvider(s.provider))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode static catalog: %v", err)
	}

	return &proto.ProviderMetadata{
		Name:                   s.provider.Name(),
		DisplayName:            s.provider.DisplayName(),
		Description:            s.provider.Description(),
		ConnectionMode:         coreConnectionModeToProto(s.provider.ConnectionMode()),
		AuthTypes:              authTypesForProvider(s.provider),
		ConnectionParams:       connectionParamDefsToProto(defs),
		StaticCatalogJson:      staticCatalog,
		SupportsSessionCatalog: supportsSessionCatalog(s.provider),
	}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *proto.ExecuteRequest) (*proto.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	return &proto.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	scp, ok := s.provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
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

func authTypesForProvider(prov core.Provider) []string {
	if atl, ok := prov.(core.AuthTypeLister); ok {
		return slices.Clone(atl.AuthTypes())
	}
	_, hasOAuth := prov.(core.OAuthProvider)
	hasManual := false
	if mp, ok := prov.(core.ManualProvider); ok {
		hasManual = mp.SupportsManualAuth()
	}
	switch {
	case hasOAuth && hasManual:
		return []string{"oauth", "manual"}
	case hasManual:
		return []string{"manual"}
	default:
		return []string{"oauth"}
	}
}

func staticCatalogForProvider(prov core.Provider) *catalog.Catalog {
	return prov.Catalog()
}

func supportsSessionCatalog(prov core.Provider) bool {
	_, ok := prov.(core.SessionCatalogProvider)
	return ok
}
