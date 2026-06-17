package core

import "context"

const BearerScheme = "Bearer "

// DefaultOAuthClientID is the first-party OAuth client identifier gestaltd uses
// when calling AuthenticationProvider.Authorize and Token.
const DefaultOAuthClientID = "gestaltd"

const (
	GrantTypeAuthorizationCode  = "authorization_code"
	GrantTypeTokenExchange      = "urn:ietf:params:oauth:grant-type:token-exchange"
	SubjectTokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
)

// AuthenticationProvider is the RFC 6749 / RFC 7662 / OIDF Grant Management
// authentication surface. Providers own subjects, grants, token issuance,
// storage, introspection, and revocation.
type AuthenticationProvider interface {
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
}

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
	ExpiresIn    int
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

// ListGrantsResponse returns caller-visible API-token grant IDs.
type ListGrantsResponse struct {
	GrantIDs []string
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
