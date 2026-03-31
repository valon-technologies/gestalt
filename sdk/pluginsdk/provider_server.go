package pluginsdk

import (
	"context"
	"slices"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginsdk/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ConfigSchemaProvider interface {
	ConfigSchemaJSON() string
}

type ProtocolVersionProvider interface {
	ProtocolVersionRange() (min, max int32)
}

type ProviderServer struct {
	pluginapiv1.UnimplementedProviderPluginServer
	provider Provider
}

func NewProviderServer(provider Provider) *ProviderServer {
	return &ProviderServer{provider: provider}
}

func (s *ProviderServer) StartProvider(ctx context.Context, req *pluginapiv1.StartProviderRequest) (*pluginapiv1.StartProviderResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if starter, ok := s.provider.(ProviderStarter); ok {
		if err := starter.Start(ctx, req.GetName(), mapFromStruct(req.GetConfig())); err != nil {
			return nil, status.Errorf(codes.Unknown, "start provider: %v", err)
		}
	}
	return &pluginapiv1.StartProviderResponse{
		ProtocolVersion: pluginapiv1.CurrentProtocolVersion,
	}, nil
}

func (s *ProviderServer) GetMetadata(_ context.Context, _ *emptypb.Empty) (*pluginapiv1.ProviderMetadata, error) {
	var connParams map[string]*pluginapiv1.ConnectionParamDef
	if cpp, ok := s.provider.(ConnectionParamProvider); ok {
		connParams = connectionParamDefsToProto(cpp.ConnectionParamDefs())
	}
	var configSchema string
	if csp, ok := s.provider.(ConfigSchemaProvider); ok {
		configSchema = csp.ConfigSchemaJSON()
	}
	minPV, maxPV := protocolVersionRange(s.provider)

	return &pluginapiv1.ProviderMetadata{
		Name:               s.provider.Name(),
		DisplayName:        s.provider.DisplayName(),
		Description:        s.provider.Description(),
		ConnectionMode:     coreConnectionModeToProto(s.provider.ConnectionMode()),
		ConnectionParams:   connParams,
		ConfigSchemaJson:   configSchema,
		MinProtocolVersion: minPV,
		MaxProtocolVersion: maxPV,
		AuthTypes:          authTypes(s.provider),
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

func authTypes(p Provider) []string {
	if atl, ok := p.(AuthTypeLister); ok {
		return slices.Clone(atl.AuthTypes())
	}
	if mp, ok := p.(ManualAuthProvider); ok && mp.SupportsManualAuth() {
		return []string{"manual"}
	}
	return nil
}

func protocolVersionRange(p Provider) (min, max int32) {
	if pvp, ok := p.(ProtocolVersionProvider); ok {
		return pvp.ProtocolVersionRange()
	}
	return pluginapiv1.CurrentProtocolVersion, pluginapiv1.CurrentProtocolVersion
}
