package gestalt

import (
	"context"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type AuthServer struct {
	proto.UnimplementedAuthPluginServer
	auth AuthProvider
}

func NewAuthProviderServer(auth AuthProvider) *AuthServer {
	return &AuthServer{auth: auth}
}

func (s *AuthServer) BeginLogin(ctx context.Context, req *proto.BeginLoginRequest) (*proto.BeginLoginResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.auth.BeginLogin(ctx, BeginLoginRequest{
		CallbackURL: req.GetCallbackUrl(),
		HostState:   req.GetHostState(),
		Scopes:      append([]string(nil), req.GetScopes()...),
		Options:     cloneStringMap(req.GetOptions()),
	})
	if err != nil {
		return nil, providerRPCError("begin login", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "auth provider returned nil response")
	}
	return &proto.BeginLoginResponse{
		AuthorizationUrl: resp.AuthorizationURL,
		PluginState:      append([]byte(nil), resp.ProviderState...),
	}, nil
}

func (s *AuthServer) CompleteLogin(ctx context.Context, req *proto.CompleteLoginRequest) (*proto.AuthenticatedUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.auth.CompleteLogin(ctx, CompleteLoginRequest{
		Query:         cloneStringMap(req.GetQuery()),
		ProviderState: append([]byte(nil), req.GetPluginState()...),
		CallbackURL:   req.GetCallbackUrl(),
	})
	if err != nil {
		return nil, providerRPCError("complete login", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "auth provider returned nil user")
	}
	return authenticatedUserToProto(user), nil
}

func (s *AuthServer) ValidateExternalToken(ctx context.Context, req *proto.ValidateExternalTokenRequest) (*proto.AuthenticatedUser, error) {
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
	return authenticatedUserToProto(user), nil
}

func (s *AuthServer) GetSessionSettings(context.Context, *emptypb.Empty) (*proto.AuthSessionSettings, error) {
	provider, ok := s.auth.(SessionTTLProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "auth provider does not expose session settings")
	}
	ttl := provider.SessionTTL()
	if ttl < 0 {
		ttl = 0
	}
	return &proto.AuthSessionSettings{
		SessionTtlSeconds: int64(ttl / time.Second),
	}, nil
}

func authenticatedUserToProto(user *AuthenticatedUser) *proto.AuthenticatedUser {
	if user == nil {
		return nil
	}
	return &proto.AuthenticatedUser{
		Subject:       user.Subject,
		Email:         user.Email,
		EmailVerified: user.EmailVerified,
		DisplayName:   user.DisplayName,
		AvatarUrl:     user.AvatarURL,
		Claims:        cloneStringMap(user.Claims),
	}
}
