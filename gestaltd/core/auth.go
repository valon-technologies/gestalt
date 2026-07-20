package core

import "context"

const BearerScheme = "Bearer "

// DefaultOAuthClientID is the first-party OAuth client identifier gestaltd uses
// when calling IdentityProvider.Authorize and Token.
const DefaultOAuthClientID = "gestaltd"

const (
	GrantTypeAuthorizationCode  = "authorization_code"
	GrantTypeTokenExchange      = "urn:ietf:params:oauth:grant-type:token-exchange"
	SubjectTokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
	// SubjectTokenTypeGestaltSubject is the RFC 8693 subject_token_type for the
	// canonical Gestalt subject ID. It is a fallback only: when a live caller
	// bearer exists on the request path, token exchange passes that actual token
	// with SubjectTokenTypeAccessToken (the provider issued it and can validate
	// it). This constant covers host-initiated flows with no bearer to present
	// (background refresh, workflow and service-account principals), where the
	// subject token is the canonical subject ID arriving over the trusted host
	// channel.
	SubjectTokenTypeGestaltSubject = "urn:gestalt:token-type:subject"
)

// MaxTokenExpiresInSeconds is the upper bound a caller may request for a
// long-lived API token lifetime. One year matches the OIDC provider's clamp.
const MaxTokenExpiresInSeconds = int64(365 * 24 * 60 * 60)

// IdentityProvider is the RFC 6749 / RFC 7662 / OIDF Grant Management
// authentication surface. Providers own subjects, grants, token issuance,
// storage, introspection, and revocation.
type IdentityProvider interface {
	// Authorize implements the RFC 6749 authorization endpoint.
	Authorize(ctx context.Context, req *AuthorizeRequest) (*AuthorizeResponse, error)

	// Token implements the RFC 6749 token endpoint.
	Token(ctx context.Context, req *TokenRequest) (*TokenResponse, error)

	// Introspect implements the RFC 7662 token introspection endpoint.
	//
	// Contract:
	//   - active=true with a populated Subject means the token is valid for this
	//     configured Gestalt deployment/API. The provider owns upstream
	//     issuer/audience/deployment validation internally.
	//   - active=false with nil error means the token is invalid, expired, or revoked.
	//   - nil response with non-nil error means provider/storage/transport failure.
	//     gestaltd treats that as a server error, not an invalid token.
	Introspect(ctx context.Context, req *IntrospectRequest) (*IntrospectResponse, error)

	// ListGrants returns grant IDs for caller-visible, user-managed API tokens
	// created via token exchange. It must not include transient login or session
	// grants.
	ListGrants(ctx context.Context, req *ListGrantsRequest) (*ListGrantsResponse, error)

	// GetGrant retrieves OIDF-shaped details for one API-token grant visible to
	// the caller. Non-visible, non-owned, or session/login grants must be
	// reported as not found.
	GetGrant(ctx context.Context, req *GetGrantRequest) (*GetGrantResponse, error)

	// RevokeGrant revokes one caller-visible API-token grant and invalidates its
	// credentials. Session/login grants must be reported as not found.
	RevokeGrant(ctx context.Context, req *RevokeGrantRequest) (*RevokeGrantResponse, error)

	// UserInfo returns profile claims for the authenticated end user represented
	// by the access token, modeled after OIDC UserInfo.
	//
	// Contract:
	//   - nil error with populated UserInfoResponse means profile data was found.
	//   - core.ErrNotFound means no profile data is stored for the token.
	//   - other errors mean provider/storage/transport failure.
	UserInfo(ctx context.Context, req *UserInfoRequest) (*UserInfoResponse, error)
}

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
	ExpiresIn    int
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

// ListGrantsResponse returns caller-visible API-token grants.
//
// GrantIDs is retained for backward compatibility and deprecated; new callers
// read Grants. Removed in XC-5.
type ListGrantsResponse struct {
	GrantIDs []string
	Grants   []GrantSummary
}

// GetGrantRequest retrieves one grant by ID.
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

// RevokeGrantRequest revokes one grant by ID.
type RevokeGrantRequest struct {
	GrantID string
}

// RevokeGrantResponse acknowledges grant revocation.
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
