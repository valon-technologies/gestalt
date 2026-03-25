package pluginsdk

import (
	"context"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProviderServer struct {
	pluginapiv1.UnimplementedProviderPluginServer
	provider Provider
}

func NewProviderServer(provider Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ProviderMetadata, error) {
	var connParams map[string]*pluginapiv1.ConnectionParamDef
	if cpp, ok := s.provider.(ConnectionParamProvider); ok {
		connParams = connectionParamDefsToProto(cpp.ConnectionParamDefs())
	}

	return &pluginapiv1.ProviderMetadata{
		Name:             s.provider.Name(),
		DisplayName:      s.provider.DisplayName(),
		Description:      s.provider.Description(),
		ConnectionMode:   coreConnectionModeToProto(s.provider.ConnectionMode()),
		ConnectionParams: connParams,
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
		ctx = WithConnectionParams(ctx, req.GetConnectionParams())
	}
	result, err := s.provider.Execute(ctx, req.GetOperation(), mapFromStruct(req.GetParams()), req.GetToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "execute: %v", err)
	}
	if result == nil {
		return nil, status.Error(codes.Internal, "provider returned nil result")
	}
	return &pluginapiv1.OperationResult{
		Status: int32(result.Status),
		Body:   result.Body,
	}, nil
}
