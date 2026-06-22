package gestalt

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

// IdentityProvider serves the Gestalt identity protocol (OAuth 2.0 / OpenID
// Connect). It issues and resolves identity-bearing tokens, exposes
// UserInfo/claims, manages grants, and supplies canonical principals.
type IdentityProvider interface {
	Provider
	Authorize(ctx context.Context, req *AuthorizeRequest) (*AuthorizeResponse, error)
	Token(ctx context.Context, req *TokenRequest) (*TokenResponse, error)
	Introspect(ctx context.Context, req *IntrospectRequest) (*IntrospectResponse, error)
	UserInfo(ctx context.Context, req *UserInfoRequest) (*UserInfoResponse, error)
	ListGrants(ctx context.Context, req *ListGrantsRequest) (*ListGrantsResponse, error)
	GetGrant(ctx context.Context, req *GetGrantRequest) (*GetGrantResponse, error)
	RevokeGrant(ctx context.Context, req *RevokeGrantRequest) (*RevokeGrantResponse, error)
}

type identityCallContextKey struct{}

// IdentityCallContext carries caller-scoped identity metadata for grant RPCs
// without widening the RFC-shaped request structs.
type IdentityCallContext struct {
	CallerBearerToken string
	Introspection     *IntrospectResponse
}

// WithIdentityCallContext returns a child context carrying caller identity metadata.
func WithIdentityCallContext(ctx context.Context, call IdentityCallContext) context.Context {
	if strings.TrimSpace(call.CallerBearerToken) == "" && call.Introspection == nil {
		return ctx
	}
	return context.WithValue(ctx, identityCallContextKey{}, call)
}

// IdentityCallContextFromContext extracts caller identity metadata from ctx.
func IdentityCallContextFromContext(ctx context.Context) IdentityCallContext {
	call, _ := ctx.Value(identityCallContextKey{}).(IdentityCallContext)
	return call
}

// AppendIdentityCallMetadata attaches caller identity metadata to outgoing gRPC metadata.
func AppendIdentityCallMetadata(ctx context.Context) context.Context {
	call := IdentityCallContextFromContext(ctx)
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
