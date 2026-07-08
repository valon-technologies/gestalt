package publicrpc

import (
	"context"
	"sync"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

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

var loadGeneratedRegistry = sync.OnceValues(NewGeneratedRegistry)

// RegisterPublicAppServer registers only PUBLIC App methods.
func RegisterPublicAppServer(s grpc.ServiceRegistrar, srv proto.AppServer) {
	registerPublic(s, srv, proto.App_ServiceDesc)
}

// RegisterPublicAgentServer registers only PUBLIC Agent methods.
func RegisterPublicAgentServer(s grpc.ServiceRegistrar, srv proto.AgentServer) {
	registerPublic(s, srv, proto.Agent_ServiceDesc)
}

// RegisterPublicWorkflowServer registers only PUBLIC Workflow methods.
func RegisterPublicWorkflowServer(s grpc.ServiceRegistrar, srv proto.WorkflowServer) {
	registerPublic(s, srv, proto.Workflow_ServiceDesc)
}

func registerPublic(s grpc.ServiceRegistrar, srv any, desc grpc.ServiceDesc) {
	reg, err := loadGeneratedRegistry()
	if err != nil {
		panic(err)
	}
	methods := make([]grpc.MethodDesc, 0, len(desc.Methods))
	for _, method := range desc.Methods {
		fullMethod := "/" + desc.ServiceName + "/" + method.MethodName
		if _, ok := reg.Lookup(fullMethod); !ok {
			continue
		}
		methods = append(methods, grpc.MethodDesc{
			MethodName: method.MethodName,
			Handler:    wrapPublicHandler(fullMethod, method.Handler),
		})
	}
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: desc.ServiceName,
		HandlerType: desc.HandlerType,
		Methods:     methods,
		Streams:     []grpc.StreamDesc{},
		Metadata:    desc.Metadata,
	}, srv)
}

func wrapPublicHandler(fullMethod string, handler grpc.MethodHandler) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		return handler(srv, WithPublicOrigin(ctx, fullMethod), dec, interceptor)
	}
}
