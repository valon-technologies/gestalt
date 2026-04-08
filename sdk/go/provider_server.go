package gestalt

import (
	"context"
	"net/http"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ProviderServer adapts a [Provider] implementation to the gRPC
// ProviderPlugin service. Most plugin authors should use [ServeProvider]
// instead of constructing this directly.
type ProviderServer struct {
	proto.UnimplementedProviderPluginServer
	provider executableProvider
}

// NewProviderServer wraps a [Provider] and typed router in a [ProviderServer]
// ready to be registered on a gRPC server.
func NewProviderServer[P any, PP interface {
	*P
	Provider
}](provider PP, router *Router[P]) *ProviderServer {
	return &ProviderServer{
		provider: &routedProvider[P, PP]{
			provider: provider,
			router:   router,
		},
	}
}

func (s *ProviderServer) StartProvider(ctx context.Context, req *proto.StartProviderRequest) (*proto.StartProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.provider.Configure(ctx, req.GetName(), configFromRequest(req.GetConfig())); err != nil {
		return nil, status.Errorf(codes.Unknown, "configure provider: %v", err)
	}
	return &proto.StartProviderResponse{
		ProtocolVersion: proto.CurrentProtocolVersion,
	}, nil
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
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return operationResultProto(operationResultFromError(err)), nil
	}
	if result == nil {
		return operationResultProto(operationResult(http.StatusInternalServerError, nilResultMessage)), nil
	}
	return operationResultProto(result), nil
}

func (s *ProviderServer) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest) (*proto.GetSessionCatalogResponse, error) {
	scp, ok := s.provider.sessionCatalogProvider()
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

func supportsSessionCatalog(p executableProvider) bool {
	_, ok := p.sessionCatalogProvider()
	return ok
}

func operationResultProto(result *OperationResult) *proto.OperationResult {
	if result == nil {
		return nil
	}
	return &proto.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}
}
