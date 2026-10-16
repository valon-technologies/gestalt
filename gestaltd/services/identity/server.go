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
	return identityResponse(resp, err, "authorize", authorizeResponseToProto)
}

func (s *providerServer) Token(ctx context.Context, req *proto.TokenRequest) (*proto.TokenResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.Token(gestalt.AuthCallContextFromIncoming(ctx), tokenRequestFromProto(req))
	return identityResponse(resp, err, "token", tokenResponseToProto)
}

func (s *providerServer) Introspect(ctx context.Context, req *proto.IntrospectRequest) (*proto.IntrospectResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.Introspect(ctx, introspectRequestFromProto(req))
	return identityResponse(resp, err, "introspect", introspectResponseToProto)
}

func (s *providerServer) UserInfo(ctx context.Context, req *proto.UserInfoRequest) (*proto.UserInfoResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.UserInfo(gestalt.AuthCallContextFromIncoming(ctx), userInfoRequestFromProto(req))
	return identityResponse(resp, err, "userinfo", userInfoResponseToProto)
}

func (s *providerServer) ListGrants(ctx context.Context, _ *proto.ListGrantsRequest) (*proto.ListGrantsResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	resp, err := s.provider.ListGrants(gestalt.AuthCallContextFromIncoming(ctx), &core.ListGrantsRequest{})
	return identityResponse(resp, err, "list grants", listGrantsResponseToProto)
}

func (s *providerServer) GetGrant(ctx context.Context, req *proto.GetGrantRequest) (*proto.GetGrantResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	resp, err := s.provider.GetGrant(gestalt.AuthCallContextFromIncoming(ctx), getGrantRequestFromProto(req))
	return identityResponse(resp, err, "get grant", getGrantResponseToProto)
}

func (s *providerServer) RevokeGrant(ctx context.Context, req *proto.RevokeGrantRequest) (*proto.RevokeGrantResponse, error) {
	if err := s.requireProvider(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if _, err := s.provider.RevokeGrant(gestalt.AuthCallContextFromIncoming(ctx), revokeGrantRequestFromProto(req)); err != nil {
		return nil, identityToGRPCError("revoke grant", err)
	}
	return &proto.RevokeGrantResponse{}, nil
}

func identityResponse[T, R any](resp *T, err error, operation string, encode func(*T) *R) (*R, error) {
	if err != nil {
		return nil, identityToGRPCError(operation, err)
	}
	if resp == nil {
		return nil, status.Error(codes.Internal, "identity provider returned nil response")
	}
	return encode(resp), nil
}

func (s *providerServer) requireProvider() error {
	if s == nil || s.provider == nil {
		return status.Error(codes.FailedPrecondition, "identity provider is not configured")
	}
	return nil
}

func identityToGRPCError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, core.ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	if code, ok := gestalt.StatusCodeOf(err); ok {
		return status.Error(identityGRPCCode(code), err.Error())
	}
	if providerStatus, ok := status.FromError(err); ok && providerStatus.Code() != codes.Unknown {
		return providerStatus.Err()
	}
	return status.Errorf(codes.Unknown, "%s: %v", operation, err)
}

func identityGRPCCode(code gestalt.StatusCode) codes.Code {
	switch code {
	case gestalt.CodeCanceled:
		return codes.Canceled
	case gestalt.CodeInvalidArgument:
		return codes.InvalidArgument
	case gestalt.CodeNotFound:
		return codes.NotFound
	case gestalt.CodeAlreadyExists:
		return codes.AlreadyExists
	case gestalt.CodeFailedPrecondition:
		return codes.FailedPrecondition
	case gestalt.CodeOutOfRange:
		return codes.OutOfRange
	case gestalt.CodeUnauthenticated:
		return codes.Unauthenticated
	case gestalt.CodePermissionDenied:
		return codes.PermissionDenied
	case gestalt.CodeUnavailable:
		return codes.Unavailable
	case gestalt.CodeUnimplemented:
		return codes.Unimplemented
	case gestalt.CodeInternal:
		return codes.Internal
	default:
		return codes.Unknown
	}
}

func authorizeRequestFromProto(req *proto.AuthorizeRequest) *core.AuthorizeRequest {
	return &core.AuthorizeRequest{
		ResponseType: req.GetResponseType(),
		ClientID:     req.GetClientId(),
		RedirectURI:  req.GetRedirectUri(),
		Scope:        req.GetScope(),
		State:        req.GetState(),
	}
}

func authorizeResponseToProto(resp *core.AuthorizeResponse) *proto.AuthorizeResponse {
	return &proto.AuthorizeResponse{RedirectUri: resp.RedirectURI}
}

func tokenRequestFromProto(req *proto.TokenRequest) *core.TokenRequest {
	return &core.TokenRequest{
		GrantType:        req.GetGrantType(),
		Code:             req.GetCode(),
		RedirectURI:      req.GetRedirectUri(),
		ClientID:         req.GetClientId(),
		State:            req.GetState(),
		Scope:            req.GetScope(),
		SubjectToken:     req.GetSubjectToken(),
		SubjectTokenType: req.GetSubjectTokenType(),
		ExpiresIn:        req.GetExpiresIn(),
		Name:             req.GetName(),
		GrantSubject:     req.GetGrantSubject(),
	}
}

func tokenResponseToProto(resp *core.TokenResponse) *proto.TokenResponse {
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
	return &core.IntrospectRequest{
		Token:         req.GetToken(),
		TokenTypeHint: req.GetTokenTypeHint(),
	}
}

func introspectResponseToProto(resp *core.IntrospectResponse) *proto.IntrospectResponse {
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
	return &proto.UserInfoResponse{
		SubjectId: resp.SubjectID,
		Email:     resp.Email,
		Name:      resp.Name,
	}
}

func listGrantsResponseToProto(resp *core.ListGrantsResponse) *proto.ListGrantsResponse {
	return &proto.ListGrantsResponse{GrantIds: append([]string(nil), resp.GrantIDs...)}
}

func getGrantRequestFromProto(req *proto.GetGrantRequest) *core.GetGrantRequest {
	return &core.GetGrantRequest{GrantID: req.GetGrantId()}
}

func getGrantResponseToProto(resp *core.GetGrantResponse) *proto.GetGrantResponse {
	out := &proto.GetGrantResponse{
		CreatedAt: resp.CreatedAt,
		ExpiresAt: resp.ExpiresAt,
		Name:      resp.Name,
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
	return &core.RevokeGrantRequest{GrantID: req.GetGrantId()}
}
