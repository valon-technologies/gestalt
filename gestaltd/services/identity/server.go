package identity

import (
	"context"
	"errors"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type providerServer struct {
	proto.UnimplementedIdentityServer
	provider core.IdentityProvider
}

func NewProviderServer(provider core.IdentityProvider) proto.IdentityServer {
	return &providerServer{provider: provider}
}

func (s *providerServer) Authorize(ctx context.Context, req *proto.AuthorizeRequest) (*proto.AuthorizeResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.Authorize(ctx, authorizeRequestFromProto(req))
	if err != nil {
		return nil, identityToGRPCError("authorize", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return authorizeResponseToProto(resp), nil
}

func (s *providerServer) Token(ctx context.Context, req *proto.TokenRequest) (*proto.TokenResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.Token(ctx, tokenRequestFromProto(req))
	if err != nil {
		return nil, identityToGRPCError("token", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return tokenResponseToProto(resp), nil
}

func (s *providerServer) Introspect(ctx context.Context, req *proto.IntrospectRequest) (*proto.IntrospectResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.Introspect(ctx, introspectRequestFromProto(req))
	if err != nil {
		return nil, identityToGRPCError("introspect", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return introspectResponseToProto(resp), nil
}

func (s *providerServer) UserInfo(ctx context.Context, req *proto.UserInfoRequest) (*proto.UserInfoResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.UserInfo(authCallContext(ctx), userInfoRequestFromProto(req))
	if err != nil {
		return nil, identityToGRPCError("userinfo", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return userInfoResponseToProto(resp), nil
}

func (s *providerServer) ListGrants(ctx context.Context, _ *proto.ListGrantsRequest) (*proto.ListGrantsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	resp, err := s.provider.ListGrants(authCallContext(ctx), &core.ListGrantsRequest{})
	if err != nil {
		return nil, identityToGRPCError("list grants", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return listGrantsResponseToProto(resp), nil
}

func (s *providerServer) GetGrant(ctx context.Context, req *proto.GetGrantRequest) (*proto.GetGrantResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.GetGrant(authCallContext(ctx), getGrantRequestFromProto(req))
	if err != nil {
		return nil, identityToGRPCError("get grant", err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return getGrantResponseToProto(resp), nil
}

func (s *providerServer) RevokeGrant(ctx context.Context, req *proto.RevokeGrantRequest) (*proto.RevokeGrantResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if _, err := s.provider.RevokeGrant(authCallContext(ctx), revokeGrantRequestFromProto(req)); err != nil {
		return nil, identityToGRPCError("revoke grant", err)
	}
	return &proto.RevokeGrantResponse{}, nil
}

func (s *providerServer) requireProvider() error {
	if s == nil || s.provider == nil {
		return status.Error(codes.FailedPrecondition, "identity provider is not configured")
	}
	return nil
}

func authCallContext(ctx context.Context) context.Context {
	token := gestalt.CallerBearerTokenFromIncomingContext(ctx)
	if token == "" {
		return ctx
	}
	return gestalt.WithIdentityCallContext(ctx, gestalt.IdentityCallContext{CallerBearerToken: token})
}

func identityToGRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, core.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}

func authorizeRequestFromProto(req *proto.AuthorizeRequest) *core.AuthorizeRequest {
	if req == nil {
		return nil
	}
	return &core.AuthorizeRequest{
		ResponseType: req.GetResponseType(),
		ClientID:     req.GetClientId(),
		RedirectURI:  req.GetRedirectUri(),
		Scope:        req.GetScope(),
		State:        req.GetState(),
	}
}

func authorizeResponseToProto(resp *core.AuthorizeResponse) *proto.AuthorizeResponse {
	if resp == nil {
		return nil
	}
	return &proto.AuthorizeResponse{RedirectUri: resp.RedirectURI}
}

func tokenRequestFromProto(req *proto.TokenRequest) *core.TokenRequest {
	if req == nil {
		return nil
	}
	return &core.TokenRequest{
		GrantType:        req.GetGrantType(),
		Code:             req.GetCode(),
		RedirectURI:      req.GetRedirectUri(),
		ClientID:         req.GetClientId(),
		State:            req.GetState(),
		Scope:            req.GetScope(),
		SubjectToken:     req.GetSubjectToken(),
		SubjectTokenType: req.GetSubjectTokenType(),
	}
}

func tokenResponseToProto(resp *core.TokenResponse) *proto.TokenResponse {
	if resp == nil {
		return nil
	}
	return &proto.TokenResponse{
		AccessToken:  resp.AccessToken,
		TokenType:    resp.TokenType,
		ExpiresIn:    int64(resp.ExpiresIn),
		RefreshToken: resp.RefreshToken,
		Scope:        resp.Scope,
		GrantId:      resp.GrantID,
	}
}

func introspectRequestFromProto(req *proto.IntrospectRequest) *core.IntrospectRequest {
	if req == nil {
		return nil
	}
	return &core.IntrospectRequest{
		Token:         req.GetToken(),
		TokenTypeHint: req.GetTokenTypeHint(),
	}
}

func introspectResponseToProto(resp *core.IntrospectResponse) *proto.IntrospectResponse {
	if resp == nil {
		return nil
	}
	return &proto.IntrospectResponse{
		Active:   resp.Active,
		Subject:  resp.Subject,
		Scope:    resp.Scope,
		ClientId: resp.ClientID,
		Audience: append([]string(nil), resp.Audience...),
	}
}

func userInfoRequestFromProto(_ *proto.UserInfoRequest) *core.UserInfoRequest {
	return &core.UserInfoRequest{}
}

func userInfoResponseToProto(resp *core.UserInfoResponse) *proto.UserInfoResponse {
	if resp == nil {
		return nil
	}
	return &proto.UserInfoResponse{
		SubjectId: resp.SubjectID,
		Email:     resp.Email,
		Name:      resp.Name,
	}
}

func listGrantsResponseToProto(resp *core.ListGrantsResponse) *proto.ListGrantsResponse {
	if resp == nil {
		return nil
	}
	return &proto.ListGrantsResponse{GrantIds: append([]string(nil), resp.GrantIDs...)}
}

func getGrantRequestFromProto(req *proto.GetGrantRequest) *core.GetGrantRequest {
	if req == nil {
		return nil
	}
	return &core.GetGrantRequest{GrantID: req.GetGrantId()}
}

func getGrantResponseToProto(resp *core.GetGrantResponse) *proto.GetGrantResponse {
	if resp == nil {
		return nil
	}
	out := &proto.GetGrantResponse{
		CreatedAt: resp.CreatedAt,
		ExpiresAt: resp.ExpiresAt,
	}
	for _, scope := range resp.Scopes {
		out.Scopes = append(out.Scopes, &proto.GrantScope{
			Scope:    scope.Scope,
			Resource: append([]string(nil), scope.Resource...),
		})
	}
	return out
}

func revokeGrantRequestFromProto(req *proto.RevokeGrantRequest) *core.RevokeGrantRequest {
	if req == nil {
		return nil
	}
	return &core.RevokeGrantRequest{GrantID: req.GetGrantId()}
}
