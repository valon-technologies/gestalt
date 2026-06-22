package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type authServer struct {
	proto.UnimplementedAuthenticationServer
	auth IdentityProvider
}

func newIdentityProviderServer(auth IdentityProvider) *authServer {
	return &authServer{auth: auth}
}

func (s *authServer) Authorize(ctx context.Context, req *proto.AuthorizeRequest) (*proto.AuthorizeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.auth.Authorize(ctx, authorizeRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("authorize", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return authorizeResponseToProto(resp), nil
}

func (s *authServer) Token(ctx context.Context, req *proto.TokenRequest) (*proto.TokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.auth.Token(ctx, tokenRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("token", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return tokenResponseToProto(resp), nil
}

func (s *authServer) Introspect(ctx context.Context, req *proto.IntrospectRequest) (*proto.IntrospectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.auth.Introspect(ctx, introspectRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("introspect", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return introspectResponseToProto(resp), nil
}

func (s *authServer) UserInfo(ctx context.Context, req *proto.UserInfoRequest) (*proto.UserInfoResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = s.authCallContext(ctx)
	resp, err := s.auth.UserInfo(ctx, userInfoRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("userinfo", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return userInfoResponseToProto(resp), nil
}

func (s *authServer) ListGrants(ctx context.Context, _ *proto.ListGrantsRequest) (*proto.ListGrantsResponse, error) {
	ctx = s.authCallContext(ctx)
	resp, err := s.auth.ListGrants(ctx, &ListGrantsRequest{})
	if err != nil {
		return nil, providerRPCError("list grants", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return listGrantsResponseToProto(resp), nil
}

func (s *authServer) GetGrant(ctx context.Context, req *proto.GetGrantRequest) (*proto.GetGrantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = s.authCallContext(ctx)
	resp, err := s.auth.GetGrant(ctx, getGrantRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("get grant", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "authentication provider returned nil response")
	}
	return getGrantResponseToProto(resp), nil
}

func (s *authServer) RevokeGrant(ctx context.Context, req *proto.RevokeGrantRequest) (*proto.RevokeGrantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ctx = s.authCallContext(ctx)
	_, err := s.auth.RevokeGrant(ctx, revokeGrantRequestFromProto(req))
	if err != nil {
		return nil, providerRPCError("revoke grant", err)
	}
	return &proto.RevokeGrantResponse{}, nil
}

func (s *authServer) authCallContext(ctx context.Context) context.Context {
	token := CallerBearerTokenFromIncomingContext(ctx)
	if token == "" {
		return ctx
	}
	return WithIdentityCallContext(ctx, IdentityCallContext{CallerBearerToken: token})
}
