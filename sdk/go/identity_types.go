package gestalt

import (
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	// CallerBearerTokenMetadataKey carries the original caller bearer token on
	// identity provider RPCs that require caller scoping.
	CallerBearerTokenMetadataKey = "x-gestalt-caller-bearer-token"
)

// AuthorizeRequest models RFC 6749 authorization endpoint parameters.
type AuthorizeRequest struct {
	ResponseType string
	ClientID     string
	RedirectURI  string
	Scope        string
	State        string
	// Audience is the RFC 8707 target audience this flow is for. Empty means the
	// platform default (login); a connection ID such as "github:default" starts a
	// connect flow. Same operation, audience selects the surface.
	Audience string
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
	ExpiresIn        int64
	// Audience is the RFC 8707/8693 target audience. Required for
	// grant_type=token-exchange (load-bearing for material exchange when no grant
	// exists; cross-check on grant-backed resolve). Unused on authorization_code
	// calls: the code correlates.
	Audience string
	// GrantID is the OIDF Grant Management token-endpoint grant_id. Selects the
	// grant instance for grant-backed resolve (replaces Qualifier/Instance).
	GrantID string
}

// TokenResponse models RFC 6749 token endpoint response fields.
type TokenResponse struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int64
	RefreshToken string
	Scope        string
	GrantID      string
	// Params is the RFC 6749 §5.1 extension member for interpolation params the
	// provider computes from stored grant metadata + connection config (e.g.
	// Looker {host}). Supersedes MetadataJSON-derived params when non-empty.
	Params map[string]string
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

// ListGrantsRequest lists API-token grants visible to the caller.
type ListGrantsRequest struct {
	// Audience filters grants to one connection audience. Empty lists across all
	// audiences.
	Audience string
}

// GrantSummary describes one caller-visible API-token grant at list time,
// serving catalog fan-out and the connections UI without N+1 GetGrant calls.
type GrantSummary struct {
	GrantID      string
	Audience     string
	Instance     string // was Qualifier
	MetadataJSON string // display labels (was ExternalCredential.MetadataJSON)
}

// ListGrantsResponse returns caller-visible API-token grants created via token
// exchange. It must not include transient login or session grants.
//
// GrantIDs is retained for backward compatibility and deprecated; new callers
// read Grants. Removed in XC-5.
type ListGrantsResponse struct {
	GrantIDs []string
	Grants   []GrantSummary
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
		Audience:     req.GetAudience(),
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
		ExpiresIn:        req.GetExpiresIn(),
		Audience:         req.GetAudience(),
		GrantID:          req.GetGrantId(),
	}
}

func tokenResponseToProto(resp *TokenResponse) *proto.TokenResponse {
	if resp == nil {
		return nil
	}
	out := &proto.TokenResponse{
		AccessToken:  resp.AccessToken,
		TokenType:    resp.TokenType,
		ExpiresIn:    resp.ExpiresIn,
		RefreshToken: resp.RefreshToken,
		Scope:        resp.Scope,
		GrantId:      resp.GrantID,
	}
	if resp.Params != nil {
		out.Params = make(map[string]string, len(resp.Params))
		for k, v := range resp.Params {
			out.Params[k] = v
		}
	}
	return out
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

func listGrantsRequestFromProto(req *proto.ListGrantsRequest) *ListGrantsRequest {
	if req == nil {
		return nil
	}
	return &ListGrantsRequest{Audience: req.GetAudience()}
}

func listGrantsResponseToProto(resp *ListGrantsResponse) *proto.ListGrantsResponse {
	if resp == nil {
		return nil
	}
	out := &proto.ListGrantsResponse{GrantIds: append([]string(nil), resp.GrantIDs...)}
	for _, g := range resp.Grants {
		out.Grants = append(out.Grants, &proto.GrantSummary{
			GrantId:      g.GrantID,
			Audience:     g.Audience,
			Instance:     g.Instance,
			MetadataJson: g.MetadataJSON,
		})
	}
	return out
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
