package publicrpc

import (
	"context"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
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

// Servers holds public service implementations shared by gRPC and REST surfaces.
type Servers struct {
	App                 proto.AppServer
	Agent               proto.AgentServer
	Workflow            proto.WorkflowServer
	IndexedDB           proto.IndexedDBServer
	Identity            proto.IdentityServer
	Authorization       proto.AuthorizationServer
	ExternalCredentials proto.ExternalCredentialsServer
}

type serverRegistration struct {
	server       any
	description  grpc.ServiceDesc
	registerREST func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error
}

func (s Servers) registrations() []serverRegistration {
	return []serverRegistration{
		{s.App, proto.App_ServiceDesc, proto.RegisterAppHandler},
		{s.Agent, proto.Agent_ServiceDesc, proto.RegisterAgentHandler},
		{s.Workflow, proto.Workflow_ServiceDesc, proto.RegisterWorkflowHandler},
		{s.IndexedDB, proto.IndexedDB_ServiceDesc, nil},
		{s.Identity, proto.Identity_ServiceDesc, proto.RegisterIdentityHandler},
		{s.Authorization, proto.Authorization_ServiceDesc, proto.RegisterAuthorizationHandler},
		{s.ExternalCredentials, proto.ExternalCredentials_ServiceDesc, nil},
	}
}

// RegisterPublicServers registers only PUBLIC methods for each configured server.
func RegisterPublicServers(s grpc.ServiceRegistrar, servers Servers) {
	for _, registration := range servers.registrations() {
		if registration.server != nil {
			RegisterPublicServer(s, registration.server, registration.description)
		}
	}
}

// RegisterPublicServer registers only PUBLIC methods from desc on srv.
func RegisterPublicServer(s grpc.ServiceRegistrar, srv any, desc grpc.ServiceDesc) {
	registerPublic(s, srv, desc)
}

// RegisterRESTGateway registers generated /api/v2 handlers that dispatch through
// conn. IndexedDB and ExternalCredentials are intentionally excluded.
func RegisterRESTGateway(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn, servers Servers) error {
	if mux == nil || conn == nil {
		return nil
	}
	for _, registration := range servers.registrations() {
		if registration.server != nil && registration.registerREST != nil {
			if err := registration.registerREST(ctx, mux, conn); err != nil {
				return err
			}
		}
	}
	return nil
}

var loadGeneratedRegistry = sync.OnceValues(NewGeneratedRegistry)

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
