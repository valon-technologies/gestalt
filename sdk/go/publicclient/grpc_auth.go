package publicclient

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type authClientConn struct {
	Conn grpc.ClientConnInterface
	Auth Auth
}

func (c *authClientConn) Invoke(
	ctx context.Context,
	method string,
	args any,
	reply any,
	opts ...grpc.CallOption,
) error {
	ctx, err := withAuthContext(ctx, c.Auth)
	if err != nil {
		return err
	}
	return c.Conn.Invoke(ctx, method, args, reply, opts...)
}

func (c *authClientConn) NewStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	ctx, err := withAuthContext(ctx, c.Auth)
	if err != nil {
		return nil, err
	}
	return c.Conn.NewStream(ctx, desc, method, opts...)
}

func withAuthContext(ctx context.Context, auth Auth) (context.Context, error) {
	if auth == nil {
		return ctx, nil
	}
	meta := &Request{Headers: map[string]string{}}
	if err := auth.Apply(ctx, meta); err != nil {
		return nil, err
	}
	if token := meta.Headers["Authorization"]; token != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", token), nil
	}
	return ctx, nil
}

func dialOptionsWithAuth(auth Auth) []grpc.DialOption {
	if auth == nil {
		return nil
	}
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(func(
			ctx context.Context,
			method string,
			req, reply any,
			cc *grpc.ClientConn,
			invoker grpc.UnaryInvoker,
			opts ...grpc.CallOption,
		) error {
			var err error
			ctx, err = withAuthContext(ctx, auth)
			if err != nil {
				return err
			}
			return invoker(ctx, method, req, reply, cc, opts...)
		}),
	}
}
