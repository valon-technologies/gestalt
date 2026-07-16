package publicclient

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// BoundOptions configures GestaltFromContext.
type BoundOptions struct {
	RequestContext    *protov1.RequestContext
	CallerBearerToken string
}

// GestaltFromContext returns a gRPC client bound to the host-service relay.
// It injects the provider request context and caller bearer token from ctx
// unless overridden in opts.
func GestaltFromContext(ctx context.Context, opts ...BoundOptions) (*Client, error) {
	var options BoundOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	reqCtx := options.RequestContext
	if reqCtx == nil {
		reqCtx = gestalt.RequestContextFromContext(ctx)
	}
	callerToken := strings.TrimSpace(options.CallerBearerToken)
	if callerToken == "" {
		callerToken = gestalt.IdentityCallContextFromContext(ctx).CallerBearerToken
	}

	target, token, err := host.Target("app")
	if err != nil {
		return nil, err
	}
	conn, err := host.DialService(ctx, "gestalt-public", target, token, "")
	if err != nil {
		return nil, err
	}
	wrapped := wrapBoundConn(conn, reqCtx, callerToken)
	client := NewGRPCClient(wrapped, NoAuth{})
	client.closeConn = conn
	return client, nil
}

func wrapBoundConn(
	conn *grpc.ClientConn,
	reqCtx *protov1.RequestContext,
	callerBearerToken string,
) grpc.ClientConnInterface {
	return &boundClientConn{
		Conn:              conn,
		RequestContext:    reqCtx,
		CallerBearerToken: callerBearerToken,
	}
}

type boundClientConn struct {
	Conn              grpc.ClientConnInterface
	RequestContext    *protov1.RequestContext
	CallerBearerToken string
}

func (c *boundClientConn) Invoke(
	ctx context.Context,
	method string,
	args any,
	reply any,
	opts ...grpc.CallOption,
) error {
	injectRequestContext(args, c.RequestContext)
	ctx = withCallerBearerToken(ctx, c.CallerBearerToken)
	return c.Conn.Invoke(ctx, method, args, reply, opts...)
}

func (c *boundClientConn) NewStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	ctx = withCallerBearerToken(ctx, c.CallerBearerToken)
	return c.Conn.NewStream(ctx, desc, method, opts...)
}

func injectRequestContext(req any, boundCtx *protov1.RequestContext) {
	if boundCtx == nil {
		return
	}
	msg, ok := req.(proto.Message)
	if !ok {
		return
	}
	reflect := msg.ProtoReflect()
	field := reflect.Descriptor().Fields().ByName("context")
	if field == nil || reflect.Has(field) {
		return
	}
	reflect.Set(field, protoreflect.ValueOfMessage(boundCtx.ProtoReflect()))
}

func withCallerBearerToken(ctx context.Context, token string) context.Context {
	token = strings.TrimSpace(token)
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, gestalt.CallerBearerTokenMetadataKey, token)
}
