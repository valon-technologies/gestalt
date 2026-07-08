package publicrpc

import "context"

type publicOriginKey struct{}

// PublicOrigin records that a request entered through the public gRPC surface.
type PublicOrigin struct {
	FullMethod string
}

// WithPublicOrigin marks ctx as public-originated for fullMethod.
func WithPublicOrigin(ctx context.Context, fullMethod string) context.Context {
	return context.WithValue(ctx, publicOriginKey{}, PublicOrigin{FullMethod: fullMethod})
}

// PublicOriginFromContext reports whether ctx was marked public-originated.
func PublicOriginFromContext(ctx context.Context) (PublicOrigin, bool) {
	origin, ok := ctx.Value(publicOriginKey{}).(PublicOrigin)
	return origin, ok
}
