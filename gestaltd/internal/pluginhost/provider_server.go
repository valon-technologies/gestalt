package pluginhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	proto.UnimplementedIntegrationProviderServer
	provider core.Provider
}

func NewProviderServer(provider core.Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*proto.ProviderMetadata, error) {
	return &proto.ProviderMetadata{
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
	return &proto.GetSessionCatalogResponse{Catalog: catalogToProto(cat)}, nil
}

func supportsSessionCatalog(prov core.Provider) bool {
	_, ok := prov.(core.SessionCatalogProvider)
	return ok
}
