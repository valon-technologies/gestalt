package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type authServer struct {
	proto.UnimplementedAuthenticationProviderServer
	proto.UnimplementedAuthProviderServer
	auth AuthenticationProvider
}

func newAuthenticationProviderServer(auth AuthenticationProvider) *authServer {
	return &authServer{auth: auth}
}

func (s *authServer) BeginLogin(ctx context.Context, req *proto.BeginLoginRequest) (*proto.BeginLoginResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.auth.BeginLogin(ctx, req)
	if err != nil {
		return nil, providerRPCError("begin login", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return resp, nil
}

func (s *authServer) CompleteLogin(ctx context.Context, req *proto.CompleteLoginRequest) (*proto.AuthenticatedUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.auth.CompleteLogin(ctx, req)
	if err != nil {
		return nil, providerRPCError("complete login", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil user")
	}
	return user, nil
}

func (s *authServer) ValidateExternalToken(ctx context.Context, req *proto.ValidateExternalTokenRequest) (*proto.AuthenticatedUser, error) {
	validator, ok := s.auth.(ExternalTokenValidator)
	if !ok {
		return nil, providerRPCError("validate external token", ErrExternalTokenValidationUnsupported)
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := validator.ValidateExternalToken(ctx, req.GetToken())
	if err != nil {
		return nil, providerRPCError("validate external token", err)
	}
	if user == nil {
		return nil, status.Error(codes.NotFound, "token not recognized")
	}
	return user, nil
}

func (s *authServer) GetSessionSettings(context.Context, *emptypb.Empty) (*proto.AuthSessionSettings, error) {
	provider, ok := s.auth.(SessionTTLProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "authentication provider does not expose session settings")
	}
	ttl := provider.SessionTTL()
	if ttl < 0 {
		ttl = 0
	}
	return &proto.AuthSessionSettings{
		SessionTtlSeconds: int64(ttl / time.Second),
	}, nil
}
