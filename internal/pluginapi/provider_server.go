package pluginapi

import (
	"context"
	"slices"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	pluginapiv1.UnimplementedProviderPluginServer
	provider core.Provider
}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ProviderMetadata, error) {
	var defs map[string]core.ConnectionParamDef
	if cpp, ok := s.provider.(core.ConnectionParamProvider); ok {
		defs = cpp.ConnectionParamDefs()
	}

	staticCatalog, err := catalogToJSON(staticCatalogForProvider(s.provider))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode static catalog: %v", err)
	}

	return &pluginapiv1.ProviderMetadata{
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

func (s *ProviderServer) ListOperations(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ListOperationsResponse, error) {
	ops, err := operationsToProto(s.provider.ListOperations())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode operations: %v", err)
	}
	return &pluginapiv1.ListOperationsResponse{Operations: ops}, nil
}

func (s *ProviderServer) Execute(ctx context.Context, req *pluginapiv1.ExecuteRequest) (*pluginapiv1.OperationResult, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	if id := req.GetInvocationId(); id != "" {
		ctx = WithInvocationID(ctx, id)
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	return &pluginapiv1.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *pluginapiv1.GetSessionCatalogRequest) (*pluginapiv1.GetSessionCatalogResponse, error) {
	scp, ok := s.provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support session catalogs")
	}
	if len(req.GetConnectionParams()) > 0 {
		ctx = core.WithConnectionParams(ctx, req.GetConnectionParams())
	}
	if id := req.GetInvocationId(); id != "" {
		ctx = WithInvocationID(ctx, id)
	}
	cat, err := scp.CatalogForRequest(ctx, req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "session catalog: %v", err)
	}
	raw, err := catalogToJSON(cat)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode session catalog: %v", err)
	}
	return &pluginapiv1.GetSessionCatalogResponse{CatalogJson: raw}, nil
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
	if cp, ok := prov.(core.CatalogProvider); ok {
		if cat := cp.Catalog(); cat != nil {
			return cat
		}
	}
	return nil
}

func supportsSessionCatalog(prov core.Provider) bool {
	_, ok := prov.(core.SessionCatalogProvider)
	return ok
}
