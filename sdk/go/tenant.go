package gestalt

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	// TenantIDMetadataKey carries the resolved tenant ID on provider RPCs.
	TenantIDMetadataKey = "gestalt-tenant-id"
	// TenantHostMetadataKey carries the canonical request host used for tenant resolution.
	TenantHostMetadataKey = "gestalt-tenant-host"
	// TenantBoundMetadataKey tells tenant-aware providers that the RPC must be
	// evaluated within the tenant scope.
	TenantBoundMetadataKey = "gestalt-tenant-bound"
	// TenantPrincipalIDMetadataKey carries the authenticated subject when the
	// caller has already been resolved.
	TenantPrincipalIDMetadataKey = "gestalt-tenant-principal-id"
)

type tenantScopeKey struct{}

// TenantScope is the host-resolved tenancy context for a request.
type TenantScope struct {
	TenantID    string
	Host        string
	TenantBound bool
	PrincipalID string
}

// Normalize returns a copy with canonical field values.
func (s TenantScope) Normalize() TenantScope {
	s.TenantID = strings.TrimSpace(s.TenantID)
	s.Host = strings.ToLower(strings.Trim(strings.TrimSpace(s.Host), "."))
	s.PrincipalID = strings.TrimSpace(s.PrincipalID)
	if s.TenantID != "" {
		s.TenantBound = true
	}
	return s
}

// WithTenantScope attaches tenant scope as a regular context value.
func WithTenantScope(ctx context.Context, scope TenantScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	scope = scope.Normalize()
	if scope.TenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantScopeKey{}, scope)
}

// TenantScopeFromContext returns tenant scope from context values or gRPC metadata.
func TenantScopeFromContext(ctx context.Context) (TenantScope, bool) {
	if ctx == nil {
		return TenantScope{}, false
	}
	if scope, ok := ctx.Value(tenantScopeKey{}).(TenantScope); ok {
		scope = scope.Normalize()
		return scope, scope.TenantID != ""
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if scope, ok := tenantScopeFromMetadata(md); ok {
			return scope, true
		}
	}
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if scope, ok := tenantScopeFromMetadata(md); ok {
			return scope, true
		}
	}
	return TenantScope{}, false
}

// ContextWithOutgoingTenantScope attaches tenant scope as a context value and
// outgoing gRPC metadata for provider calls derived from the returned context.
func ContextWithOutgoingTenantScope(ctx context.Context, scope TenantScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	scope = scope.Normalize()
	if scope.TenantID == "" {
		return ctx
	}
	ctx = WithTenantScope(ctx, scope)
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	setMetadataValue(md, TenantIDMetadataKey, scope.TenantID)
	setMetadataValue(md, TenantHostMetadataKey, scope.Host)
	if scope.TenantBound {
		setMetadataValue(md, TenantBoundMetadataKey, "true")
	}
	setMetadataValue(md, TenantPrincipalIDMetadataKey, scope.PrincipalID)
	return metadata.NewOutgoingContext(ctx, md)
}

func tenantScopeFromMetadata(md metadata.MD) (TenantScope, bool) {
	scope := TenantScope{
		TenantID:    firstMetadataValue(md, TenantIDMetadataKey),
		Host:        firstMetadataValue(md, TenantHostMetadataKey),
		TenantBound: strings.EqualFold(firstMetadataValue(md, TenantBoundMetadataKey), "true"),
		PrincipalID: firstMetadataValue(md, TenantPrincipalIDMetadataKey),
	}.Normalize()
	return scope, scope.TenantID != ""
}

func firstMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func setMetadataValue(md metadata.MD, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		delete(md, key)
		return
	}
	md.Set(key, value)
}
