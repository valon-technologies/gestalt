package publicrpc

import "context"

// PublicMethodRegistry resolves public method policy by gRPC full method name.
type PublicMethodRegistry interface {
	Lookup(fullMethod string) (PublicMethodPolicy, bool)
}

// IsPublicOrigin reports whether ctx was marked by generated public wrappers.
func IsPublicOrigin(ctx context.Context) bool {
	_, ok := PublicOriginFromContext(ctx)
	return ok
}

// FullMethodFromContext returns the public gRPC full method when ctx is
// public-originated.
func FullMethodFromContext(ctx context.Context) (string, bool) {
	origin, ok := PublicOriginFromContext(ctx)
	if !ok || origin.FullMethod == "" {
		return "", false
	}
	return origin.FullMethod, true
}
