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
type trustedCallerSubjectKey struct{}

// TrustedCallerSubjectMetadataKey is the metadata key for a caller subject
// already verified by gestaltd on the private provider transport.
const TrustedCallerSubjectMetadataKey = "x-gestalt-caller-proof-subject"

// WithTrustedCallerSubject stores a caller subject verified by host-service ingress.
func WithTrustedCallerSubject(ctx context.Context, subjectID string) context.Context {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ctx
	}
	return context.WithValue(ctx, trustedCallerSubjectKey{}, subjectID)
}

// TrustedCallerSubjectFromContext returns a verified caller subject when present.
func TrustedCallerSubjectFromContext(ctx context.Context) string {
	subjectID, _ := ctx.Value(trustedCallerSubjectKey{}).(string)
	return strings.TrimSpace(subjectID)
}

// IdentityCallContext carries caller-scoped identity metadata for grant RPCs
// without widening the RFC-shaped request structs.
type IdentityCallContext struct {
	CallerBearerToken string
	CallerSubjectID   string
	Introspection     *IntrospectResponse
}

// WithIdentityCallContext returns a child context carrying caller identity metadata.
func WithIdentityCallContext(ctx context.Context, call IdentityCallContext) context.Context {
	if strings.TrimSpace(call.CallerBearerToken) == "" &&
		strings.TrimSpace(call.CallerSubjectID) == "" &&
		call.Introspection == nil {
		return ctx
	}
	return context.WithValue(ctx, identityCallContextKey{}, call)
}

// IdentityCallContextFromContext extracts caller identity metadata from ctx.
func IdentityCallContextFromContext(ctx context.Context) IdentityCallContext {
	call, _ := ctx.Value(identityCallContextKey{}).(IdentityCallContext)
	return call
}

// AuthCallContextFromIncoming builds identity call context from verified relay
// state or legacy caller bearer metadata.
func AuthCallContextFromIncoming(ctx context.Context) context.Context {
	if subjectID := TrustedCallerSubjectFromContext(ctx); subjectID != "" {
		return WithIdentityCallContext(ctx, IdentityCallContext{
			CallerSubjectID: subjectID,
		})
	}
	if subjectID := trustedCallerSubjectFromIncomingMetadata(ctx); subjectID != "" {
		return WithIdentityCallContext(ctx, IdentityCallContext{
			CallerSubjectID: subjectID,
		})
	}
	if token := CallerBearerTokenFromIncomingContext(ctx); token != "" {
		return WithIdentityCallContext(ctx, IdentityCallContext{CallerBearerToken: token})
	}
	return ctx
}

// AppendIdentityCallMetadata attaches caller identity metadata to outgoing gRPC
// metadata for the private gestaltd-to-provider transport.
func AppendIdentityCallMetadata(ctx context.Context) context.Context {
	call := IdentityCallContextFromContext(ctx)
	if token := strings.TrimSpace(call.CallerBearerToken); token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, CallerBearerTokenMetadataKey, token)
	}
	subjectID := strings.TrimSpace(call.CallerSubjectID)
	if subjectID == "" {
		subjectID = TrustedCallerSubjectFromContext(ctx)
	}
	if subjectID != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, TrustedCallerSubjectMetadataKey, subjectID)
	}
	return ctx
}

func trustedCallerSubjectFromIncomingMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	for _, value := range md.Get(TrustedCallerSubjectMetadataKey) {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
