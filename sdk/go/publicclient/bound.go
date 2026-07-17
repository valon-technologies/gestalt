package publicclient

import (
	"context"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var connectPublicAppConns host.ConnPool

// BoundClient is the App-only public client for provider host-service access.
type BoundClient struct {
	App *generated.AppClient

	transport interface {
		Close() error
	}
}

// Close releases transport resources when the client owns them.
func (c *BoundClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// GestaltFromContext returns an App-only gRPC client bound to the host-service relay.
// It injects the provider request context and validated identity metadata from ctx.
// Bound access accepts no public address or auth configuration.
func GestaltFromContext(ctx context.Context) (*BoundClient, error) {
	reqCtx := gestalt.RequestContextFromContext(ctx)

	target, token, err := host.Target("app")
	if err != nil {
		return nil, err
	}
	conn, err := connectPublicAppConns.Conn(ctx, "gestalt-public", target, token, "")
	if err != nil {
		return nil, err
	}
	wrapped := &boundClientConn{
		ClientConnInterface: conn,
		RequestContext:      reqCtx,
	}
	grpcT := &grpcUnaryTransport{conn: wrapped}
	return &BoundClient{
		App:       generated.NewAppClient(grpcT),
		transport: grpcT,
	}, nil
}

type boundClientConn struct {
	grpc.ClientConnInterface
	RequestContext *proto.RequestContext
}

func (c *boundClientConn) Invoke(
	ctx context.Context,
	method string,
	args any,
	reply any,
	opts ...grpc.CallOption,
) error {
	injectRequestContext(args, c.RequestContext)
	ctx = appendOutboundCallerBearer(ctx)
	return c.ClientConnInterface.Invoke(ctx, method, args, reply, opts...)
}

func (c *boundClientConn) NewStream(
	ctx context.Context,
	desc *grpc.StreamDesc,
	method string,
	opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	ctx = appendOutboundCallerBearer(ctx)
	return c.ClientConnInterface.NewStream(ctx, desc, method, opts...)
}

func appendOutboundCallerBearer(ctx context.Context) context.Context {
	if token := strings.TrimSpace(
		gestalt.IdentityCallContextFromContext(ctx).CallerBearerToken,
	); token != "" {
		return gestalt.AppendIdentityCallMetadata(ctx)
	}
	if token := strings.TrimSpace(gestalt.CallerBearerTokenFromIncomingContext(ctx)); token != "" {
		return metadata.AppendToOutgoingContext(ctx, gestalt.CallerBearerTokenMetadataKey, token)
	}
	return ctx
}

func injectRequestContext(req any, boundCtx *proto.RequestContext) {
	if boundCtx == nil {
		return
	}
	msg, ok := req.(gproto.Message)
	if !ok {
		return
	}
	reflect := msg.ProtoReflect()
	field := reflect.Descriptor().Fields().ByName("context")
	if field == nil || reflect.Has(field) {
		return
	}
	cloned, _ := gproto.Clone(boundCtx).(*proto.RequestContext)
	reflect.Set(field, protoreflect.ValueOfMessage(cloned.ProtoReflect()))
}
