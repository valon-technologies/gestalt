package publicrpc

import "context"

type originContextKey struct{}

type originState struct {
	fullMethod string
}

// WithPublicOrigin marks ctx as a public/external request from generated public
// gRPC wrappers. fullMethod uses the grpc-go form, e.g.
// "/gestalt.provider.v1.App/Invoke".
func WithPublicOrigin(ctx context.Context, fullMethod string) context.Context {
	if fullMethod == "" {
		return ctx
	}
	return context.WithValue(ctx, originContextKey{}, originState{fullMethod: fullMethod})
}

// IsPublicOrigin reports whether ctx was marked by generated public wrappers.
func IsPublicOrigin(ctx context.Context) bool {
	_, ok := ctx.Value(originContextKey{}).(originState)
	return ok
}

// FullMethodFromContext returns the public gRPC full method when ctx is
// public-originated.
func FullMethodFromContext(ctx context.Context) (string, bool) {
	state, ok := ctx.Value(originContextKey{}).(originState)
	if !ok || state.fullMethod == "" {
		return "", false
	}
	return state.fullMethod, true
}
