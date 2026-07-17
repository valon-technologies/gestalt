package publicclient

import (
	"context"
	"strings"

	gestaltclient "github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
)

type grpcUnaryTransport struct {
	conn  grpc.ClientConnInterface
	owned *grpc.ClientConn
	auth  Auth
}

func (t *grpcUnaryTransport) Close() error {
	if t == nil || t.owned == nil {
		return nil
	}
	err := t.owned.Close()
	t.owned = nil
	t.conn = nil
	return err
}

func (t *grpcUnaryTransport) Unary(
	ctx context.Context,
	method generated.Method,
	request, response gproto.Message,
) error {
	if t == nil || t.conn == nil {
		return &generated.GestaltError{
			Code:    gestaltclient.GestaltErrorCodeInvalidArgument,
			Message: "publicclient: gRPC transport is nil",
		}
	}
	ctx, err := withAuthContext(ctx, t.auth)
	if err != nil {
		return err
	}
	if err := t.conn.Invoke(ctx, method.FullMethod, request, response); err != nil {
		return gestaltclient.ToGestaltError(err)
	}
	return nil
}

func withAuthContext(ctx context.Context, auth Auth) (context.Context, error) {
	if auth == nil {
		return ctx, nil
	}
	meta := &Request{Headers: map[string]string{}}
	if err := auth.Apply(ctx, meta); err != nil {
		return nil, err
	}
	for key, value := range meta.Headers {
		if value == "" {
			continue
		}
		ctx = metadata.AppendToOutgoingContext(ctx, strings.ToLower(key), value)
	}
	return ctx, nil
}
