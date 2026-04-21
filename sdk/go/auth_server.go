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
	auth PluginProvider
}

func newAuthenticationProviderServer(auth PluginProvider) *authServer {
	return &authServer{auth: auth}
}

func (s *authServer) BeginAuthentication(ctx context.Context, req *proto.BeginAuthenticationRequest) (*proto.BeginAuthenticationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.beginAuthentication(ctx, req)
	if err != nil {
		return nil, providerRPCError("begin authentication", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return resp, nil
}

func (s *authServer) CompleteAuthentication(ctx context.Context, req *proto.CompleteAuthenticationRequest) (*proto.AuthenticatedUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.completeAuthentication(ctx, req)
	if err != nil {
		return nil, providerRPCError("complete authentication", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil user")
	}
	return user, nil
}

func (s *authServer) Authenticate(ctx context.Context, req *proto.AuthenticateRequest) (*proto.AuthenticatedUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.authenticate(ctx, req)
	if err != nil {
		return nil, providerRPCError("authenticate", err)
	}
	if user == nil {
		return nil, status.Error(codes.NotFound, "authentication input not recognized")
	}
	return user, nil
}

func (s *authServer) BeginLogin(ctx context.Context, req *proto.BeginLoginRequest) (*proto.BeginLoginResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.beginLogin(ctx, req)
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
	user, err := s.completeLogin(ctx, req)
	if err != nil {
		return nil, providerRPCError("complete login", err)
	}
	if user == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil user")
	}
	return user, nil
}

func (s *authServer) ValidateExternalToken(ctx context.Context, req *proto.ValidateExternalTokenRequest) (*proto.AuthenticatedUser, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	user, err := s.validateExternalToken(ctx, req)
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

func (s *authServer) beginAuthentication(ctx context.Context, req *proto.BeginAuthenticationRequest) (*proto.BeginAuthenticationResponse, error) {
	if provider, ok := s.auth.(AuthenticationProvider); ok {
		return provider.BeginAuthentication(ctx, req)
	}
	if provider, ok := s.auth.(LegacyAuthenticationProvider); ok {
		resp, err := provider.BeginLogin(ctx, &proto.BeginLoginRequest{
			CallbackUrl: req.GetCallbackUrl(),
			HostState:   req.GetHostState(),
			Scopes:      append([]string(nil), req.GetScopes()...),
			Options:     cloneStringMap(req.GetOptions()),
		})
		if err != nil || resp == nil {
			return nil, err
		}
		return &proto.BeginAuthenticationResponse{
			AuthorizationUrl: resp.GetAuthorizationUrl(),
			ProviderState:    append([]byte(nil), resp.GetProviderState()...),
		}, nil
	}
	return nil, status.Error(codes.Unimplemented, "authentication provider does not implement begin authentication")
}

func (s *authServer) completeAuthentication(ctx context.Context, req *proto.CompleteAuthenticationRequest) (*proto.AuthenticatedUser, error) {
	if provider, ok := s.auth.(AuthenticationProvider); ok {
		return provider.CompleteAuthentication(ctx, req)
	}
	if provider, ok := s.auth.(LegacyAuthenticationProvider); ok {
		return provider.CompleteLogin(ctx, &proto.CompleteLoginRequest{
			Query:         cloneStringMap(req.GetQuery()),
			ProviderState: append([]byte(nil), req.GetProviderState()...),
			CallbackUrl:   req.GetCallbackUrl(),
		})
	}
	return nil, status.Error(codes.Unimplemented, "authentication provider does not implement complete authentication")
}

func (s *authServer) authenticate(ctx context.Context, req *proto.AuthenticateRequest) (*proto.AuthenticatedUser, error) {
	if authenticator, ok := s.auth.(Authenticator); ok {
		return authenticator.Authenticate(ctx, req)
	}
	switch input := req.GetInput().(type) {
	case *proto.AuthenticateRequest_Token:
		validator, ok := s.auth.(ExternalTokenValidator)
		if !ok {
			return nil, ErrExternalTokenValidationUnsupported
		}
		return validator.ValidateExternalToken(ctx, input.Token.GetToken())
	default:
		return nil, ErrExternalTokenValidationUnsupported
	}
}

func (s *authServer) beginLogin(ctx context.Context, req *proto.BeginLoginRequest) (*proto.BeginLoginResponse, error) {
	if provider, ok := s.auth.(LegacyAuthenticationProvider); ok {
		return provider.BeginLogin(ctx, req)
	}
	if provider, ok := s.auth.(AuthenticationProvider); ok {
		resp, err := provider.BeginAuthentication(ctx, &proto.BeginAuthenticationRequest{
			CallbackUrl: req.GetCallbackUrl(),
			HostState:   req.GetHostState(),
			Scopes:      append([]string(nil), req.GetScopes()...),
			Options:     cloneStringMap(req.GetOptions()),
		})
		if err != nil || resp == nil {
			return nil, err
		}
		return &proto.BeginLoginResponse{
			AuthorizationUrl: resp.GetAuthorizationUrl(),
			ProviderState:    append([]byte(nil), resp.GetProviderState()...),
		}, nil
	}
	return nil, status.Error(codes.Unimplemented, "authentication provider does not implement begin login")
}

func (s *authServer) completeLogin(ctx context.Context, req *proto.CompleteLoginRequest) (*proto.AuthenticatedUser, error) {
	if provider, ok := s.auth.(LegacyAuthenticationProvider); ok {
		return provider.CompleteLogin(ctx, req)
	}
	if provider, ok := s.auth.(AuthenticationProvider); ok {
		return provider.CompleteAuthentication(ctx, &proto.CompleteAuthenticationRequest{
			Query:         cloneStringMap(req.GetQuery()),
			ProviderState: append([]byte(nil), req.GetProviderState()...),
			CallbackUrl:   req.GetCallbackUrl(),
		})
	}
	return nil, status.Error(codes.Unimplemented, "authentication provider does not implement complete login")
}

func (s *authServer) validateExternalToken(ctx context.Context, req *proto.ValidateExternalTokenRequest) (*proto.AuthenticatedUser, error) {
	if validator, ok := s.auth.(ExternalTokenValidator); ok {
		return validator.ValidateExternalToken(ctx, req.GetToken())
	}
	if authenticator, ok := s.auth.(Authenticator); ok {
		return authenticator.Authenticate(ctx, &proto.AuthenticateRequest{
			Input: &proto.AuthenticateRequest_Token{
				Token: &proto.TokenAuthInput{Token: req.GetToken()},
			},
		})
	}
	return nil, ErrExternalTokenValidationUnsupported
}
