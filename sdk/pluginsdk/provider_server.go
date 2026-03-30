package pluginsdk

import (
	"context"
	"slices"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
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
	if id := req.GetInvocationId(); id != "" {
		ctx = WithInvocationID(ctx, id)
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

func (s *ProviderServer) AuthorizationURL(_ context.Context, req *pluginapiv1.AuthorizationURLRequest) (*pluginapiv1.AuthorizationURLResponse, error) {
	oauthProv, ok := s.provider.(OAuthProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support OAuth")
	}
	return &pluginapiv1.AuthorizationURLResponse{
		Url: oauthProv.AuthorizationURL(req.GetState(), slices.Clone(req.GetScopes())),
	}, nil
}

func (s *ProviderServer) ExchangeCode(ctx context.Context, req *pluginapiv1.ExchangeCodeRequest) (*pluginapiv1.TokenResponse, error) {
	oauthProv, ok := s.provider.(OAuthProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support OAuth")
	}
	resp, err := oauthProv.ExchangeCode(ctx, req.GetCode())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "exchange code: %v", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "provider returned nil token response")
	}
	msg, err := tokenResponseToProto(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode token response: %v", err)
	}
	return msg, nil
}

func (s *ProviderServer) RefreshToken(ctx context.Context, req *pluginapiv1.RefreshTokenRequest) (*pluginapiv1.TokenResponse, error) {
	oauthProv, ok := s.provider.(OAuthProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "provider does not support OAuth")
	}
	resp, err := oauthProv.RefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "refresh token: %v", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "provider returned nil token response")
	}
	msg, err := tokenResponseToProto(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode token response: %v", err)
	}
	return msg, nil
}

func authTypes(p Provider) []string {
	if atl, ok := p.(AuthTypeLister); ok {
		return slices.Clone(atl.AuthTypes())
	}
	_, hasOAuth := p.(OAuthProvider)
	hasManual := false
	if mp, ok := p.(ManualAuthProvider); ok {
		hasManual = mp.SupportsManualAuth()
	}
	switch {
	case hasOAuth && hasManual:
		return []string{"oauth", "manual"}
	case hasOAuth:
		return []string{"oauth"}
	case hasManual:
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
