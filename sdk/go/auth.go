package gestalt

import (
	"context"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/metadata"
)

const (
	// CallerBearerTokenMetadataKey carries the original caller bearer token on
	// authentication provider RPCs that require caller scoping.
	CallerBearerTokenMetadataKey = "x-gestalt-caller-bearer-token"
)

// AuthorizeRequest models RFC 6749 authorization endpoint parameters.
type AuthorizeRequest struct {
	ResponseType string
	ClientID     string
	RedirectURI  string
	Scope        string
	State        string
}

// AuthorizeResponse contains the redirect URI with RFC 6749 response parameters.
type AuthorizeResponse struct {
	RedirectURI string
}

// TokenRequest models RFC 6749 token endpoint parameters and RFC 8693 token
// exchange inputs.
type TokenRequest struct {
	GrantType        string
	Code             string
	RedirectURI      string
	ClientID         string
	State            string
	Scope            string
	SubjectToken     string
	SubjectTokenType string
}

// TokenResponse models RFC 6749 token endpoint response fields.
type TokenResponse struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int64
	RefreshToken string
	Scope        string
	GrantID      string
}

// IntrospectRequest models RFC 7662 token introspection parameters.
type IntrospectRequest struct {
	Token         string
	TokenTypeHint string
}

// IntrospectResponse models RFC 7662 token introspection response fields.
//
// Subject must be a canonical Gestalt subject ID, for example a user: subject
// using a stable user identifier or verified email. It must not be a raw
// upstream OIDC sub.
//
// Scope uses space-delimited OAuth scope values. An empty Scope means full
// first-party/Gestalt access for that grant.
type IntrospectResponse struct {
	Active   bool
	Subject  string
	Scope    string
	ClientID string
	Audience []string
}

// ListGrantsRequest lists API-token grant IDs visible to the caller.
type ListGrantsRequest struct{}

// ListGrantsResponse returns caller-visible API-token grant IDs created via
// token exchange. It must not include transient login or session grants.
type ListGrantsResponse struct {
	GrantIDs []string
}

// GetGrantRequest retrieves one API-token grant by ID.
type GetGrantRequest struct {
	GrantID string
}

// GrantScope describes one authorized scope and optional resources.
type GrantScope struct {
	Scope    string
	Resource []string
}

// GetGrantResponse returns OIDF-shaped grant details.
type GetGrantResponse struct {
	Scopes    []GrantScope
	CreatedAt int64
	ExpiresAt int64
}

// RevokeGrantRequest revokes one caller-visible API-token grant by ID.
type RevokeGrantRequest struct {
	GrantID string
}

// RevokeGrantResponse acknowledges API-token grant revocation.
type RevokeGrantResponse struct{}

// UserInfoRequest is intentionally empty. The caller bearer token is supplied
// through provider-call metadata, analogous to OIDC Authorization: Bearer.
type UserInfoRequest struct{}

// UserInfoResponse models profile claims about the authenticated end user.
type UserInfoResponse struct {
	SubjectID string
	Email     string
	Name      string
}

// AuthenticationProvider serves the Gestalt authentication protocol.
//
// ListGrants, GetGrant, and RevokeGrant are the grant-management surface used
// by Gestalt API-token management. ListGrants must return only caller-visible,
// user-managed API-token/token-exchange grants, not transient login/session
// grants. GetGrant and RevokeGrant must treat non-visible session/login grants
// as not found.
type AuthenticationProvider interface {
	Provider
	Authorize(ctx context.Context, req *AuthorizeRequest) (*AuthorizeResponse, error)
	Token(ctx context.Context, req *TokenRequest) (*TokenResponse, error)
	Introspect(ctx context.Context, req *IntrospectRequest) (*IntrospectResponse, error)
	UserInfo(ctx context.Context, req *UserInfoRequest) (*UserInfoResponse, error)
	ListGrants(ctx context.Context, req *ListGrantsRequest) (*ListGrantsResponse, error)
	GetGrant(ctx context.Context, req *GetGrantRequest) (*GetGrantResponse, error)
	RevokeGrant(ctx context.Context, req *RevokeGrantRequest) (*RevokeGrantResponse, error)
}

type authCallContextKey struct{}

// AuthCallContext carries caller-scoped authentication metadata for grant RPCs
// without widening the RFC-shaped request structs.
type AuthCallContext struct {
	CallerBearerToken string
	Introspection     *IntrospectResponse
}

// WithAuthCallContext returns a child context carrying caller auth metadata.
func WithAuthCallContext(ctx context.Context, call AuthCallContext) context.Context {
	if strings.TrimSpace(call.CallerBearerToken) == "" && call.Introspection == nil {
		return ctx
	}
	return context.WithValue(ctx, authCallContextKey{}, call)
}

// AuthCallContextFromContext extracts caller auth metadata from ctx.
func AuthCallContextFromContext(ctx context.Context) AuthCallContext {
	call, _ := ctx.Value(authCallContextKey{}).(AuthCallContext)
	return call
}

// AppendAuthCallMetadata attaches caller auth metadata to outgoing gRPC metadata.
func AppendAuthCallMetadata(ctx context.Context) context.Context {
	call := AuthCallContextFromContext(ctx)
	token := strings.TrimSpace(call.CallerBearerToken)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, CallerBearerTokenMetadataKey, token)
}

// CallerBearerTokenFromIncomingContext reads the caller bearer token from gRPC metadata.
func CallerBearerTokenFromIncomingContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(CallerBearerTokenMetadataKey) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func authorizeRequestFromProto(req *proto.AuthorizeRequest) *AuthorizeRequest {
	if req == nil {
		return nil
	}
	return &AuthorizeRequest{
		ResponseType: req.GetResponseType(),
		ClientID:     req.GetClientId(),
		RedirectURI:  req.GetRedirectUri(),
		Scope:        req.GetScope(),
		State:        req.GetState(),
	}
}

func authorizeResponseToProto(resp *AuthorizeResponse) *proto.AuthorizeResponse {
	if resp == nil {
		return nil
	}
	return &proto.AuthorizeResponse{RedirectUri: resp.RedirectURI}
}

func tokenRequestFromProto(req *proto.TokenRequest) *TokenRequest {
	if req == nil {
		return nil
	}
	return &TokenRequest{
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

func tokenResponseToProto(resp *TokenResponse) *proto.TokenResponse {
	if resp == nil {
		return nil
	}
	return &proto.TokenResponse{
		AccessToken:  resp.AccessToken,
		TokenType:    resp.TokenType,
		ExpiresIn:    resp.ExpiresIn,
		RefreshToken: resp.RefreshToken,
		Scope:        resp.Scope,
		GrantId:      resp.GrantID,
	}
}

func introspectRequestFromProto(req *proto.IntrospectRequest) *IntrospectRequest {
	if req == nil {
		return nil
	}
	return &IntrospectRequest{
		Token:         req.GetToken(),
		TokenTypeHint: req.GetTokenTypeHint(),
	}
}

func introspectResponseToProto(resp *IntrospectResponse) *proto.IntrospectResponse {
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

func listGrantsResponseToProto(resp *ListGrantsResponse) *proto.ListGrantsResponse {
	if resp == nil {
		return nil
	}
	return &proto.ListGrantsResponse{GrantIds: append([]string(nil), resp.GrantIDs...)}
}

func getGrantRequestFromProto(req *proto.GetGrantRequest) *GetGrantRequest {
	if req == nil {
		return nil
	}
	return &GetGrantRequest{GrantID: req.GetGrantId()}
}

func getGrantResponseToProto(resp *GetGrantResponse) *proto.GetGrantResponse {
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

func revokeGrantRequestFromProto(req *proto.RevokeGrantRequest) *RevokeGrantRequest {
	if req == nil {
		return nil
	}
	return &RevokeGrantRequest{GrantID: req.GetGrantId()}
}

func userInfoRequestFromProto(req *proto.UserInfoRequest) *UserInfoRequest {
	if req == nil {
		return nil
	}
	return &UserInfoRequest{}
}

func userInfoResponseToProto(resp *UserInfoResponse) *proto.UserInfoResponse {
	if resp == nil {
		return nil
	}
	return &proto.UserInfoResponse{
		SubjectId: resp.SubjectID,
		Email:     resp.Email,
		Name:      resp.Name,
	}
}
