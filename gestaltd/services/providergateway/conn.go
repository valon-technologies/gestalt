package providergateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type LocalRegistry struct {
	mu     sync.RWMutex
	direct map[ProviderTarget]DirectEndpoint
}

func NewLocalRegistry() *LocalRegistry {
	return &LocalRegistry{
		direct: make(map[ProviderTarget]DirectEndpoint),
	}
}

func (r *LocalRegistry) RegisterDirect(target ProviderTarget, endpoint DirectEndpoint) error {
	if endpoint.Desc == nil || endpoint.Server == nil {
		return fmt.Errorf("provider gateway: direct endpoint for %s/%s requires service desc and server", target.Kind, target.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.direct[target]; exists {
		return fmt.Errorf("provider gateway: direct endpoint for %s/%s already registered", target.Kind, target.Name)
	}
	r.direct[target] = endpoint
	return nil
}

// Resolve returns the direct endpoint; unknown targets report NotFound.
func (r *LocalRegistry) Resolve(_ context.Context, target ProviderTarget) (*DirectEndpoint, error) {
	r.mu.RLock()
	endpoint, ok := r.direct[target]
	r.mu.RUnlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "provider gateway: route not found")
	}
	return &endpoint, nil
}

type targetConn struct {
	gateway *routingGateway
	target  ProviderTarget
}

// Invoke runs the resolve-authorize-dispatch sequence for a unary RPC. The
// transport-path metric reflects the resolved route, so a call denied by
// authorization after a successful resolve is bucketed as direct.
func (c *targetConn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	startedAt := time.Now()
	transportPath := TransportPathUnresolved
	metricReq := metricRequest(c.target, method)

	endpoint, err := c.gateway.registry.Resolve(ctx, c.target)
	if err != nil {
		recordProviderGatewayOperation(ctx, startedAt, err, metricReq, transportPath)
		return err
	}
	transportPath = TransportPathDirect
	if err := authorizeRoutingCall(ctx, c.gateway.authorization, c.target, method); err != nil {
		recordProviderGatewayOperation(ctx, startedAt, err, metricReq, transportPath)
		return err
	}
	handlerCtx := directHandlerContext(ctx)
	invokeErr := c.dispatchUnary(handlerCtx, *endpoint, method, args, reply, opts)
	recordProviderGatewayOperation(ctx, startedAt, invokeErr, metricReq, transportPath)
	return invokeErr
}

// dispatchUnary selects the registry-aware dispatch path when a KindRegistry is
// attached, falling back to the ServiceDesc-based path otherwise. When the
// registry is attached it is authoritative for method resolution: a full method
// it does not map (or that belongs to a different kind than the target) returns
// Unimplemented, which is what makes the future PG-7 allowlist safe by
// construction. The per-target instance server wins over the kind's default
// server so direct instances (e.g. individual app providers) dispatch to their
// own implementation.
func (c *targetConn) dispatchUnary(ctx context.Context, endpoint DirectEndpoint, fullMethod string, req any, reply any, opts []grpc.CallOption) error {
	if c.gateway.kinds == nil {
		return invokeDirectUnary(ctx, endpoint, fullMethod, req, reply, opts)
	}
	km, ok := c.gateway.kinds.Lookup(fullMethod)
	if !ok || km.Kind != c.target.Kind {
		return status.Errorf(codes.Unimplemented, "provider gateway: unknown method %q", fullMethod)
	}
	server := endpoint.Server
	if server == nil {
		server = km.Server
	}
	return invokeDirectUnaryHandler(ctx, server, km.Method, fullMethod, req, reply, opts)
}

// NewStream returns Unimplemented: streaming routes move to PG-5 (issue-148).
func (c *targetConn) NewStream(ctx context.Context, _ *grpc.StreamDesc, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	startedAt := time.Now()
	err := status.Error(codes.Unimplemented,
		"providergateway: streaming routes land with PG-5 (issue-148); PG-1 is unary-only")
	recordProviderGatewayOperation(ctx, startedAt, err, metricRequest(c.target, method), TransportPathUnresolved)
	return nil, err
}

type routingGateway struct {
	registry      *LocalRegistry
	authorization AuthorizationProvider
	kinds         *KindRegistry
}

// RoutingGatewayOption configures a routing gateway at construction.
type RoutingGatewayOption func(*routingGateway)

// WithKindRegistry attaches a KindRegistry as the authoritative method
// resolver for the direct route. When attached, dispatch consults
// KindRegistry.Lookup for the method descriptor and kind's default server; the
// per-target instance server still wins when non-nil. Without an attached
// registry the gateway uses each DirectEndpoint's ServiceDesc, as in PG-1.
func WithKindRegistry(kinds *KindRegistry) RoutingGatewayOption {
	return func(g *routingGateway) { g.kinds = kinds }
}

// NewRoutingGateway returns a ProviderGateway data-plane implementation. A nil
// authorization provider skips the authorization step (optional-provider
// convention); a configured provider is consulted for non-authorization kinds.
// Options attach the kind registry and future data-plane seams.
func NewRoutingGateway(registry *LocalRegistry, authorization AuthorizationProvider, opts ...RoutingGatewayOption) Gateway {
	if registry == nil {
		panic("provider gateway: local registry is required")
	}
	g := &routingGateway{registry: registry, authorization: authorization}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	return g
}

func (g *routingGateway) Conn(target ProviderTarget) grpc.ClientConnInterface {
	return &targetConn{gateway: g, target: target}
}

func metricRequest(target ProviderTarget, method string) ProviderGatewayRequest {
	service, operation := splitFullMethod(method)
	return ProviderGatewayRequest{
		ProviderID:   target.Name,
		ProviderKind: target.Kind,
		ServiceName:  service,
		Operation:    operation,
	}
}

func authorizeRoutingCall(ctx context.Context, authorization AuthorizationProvider, target ProviderTarget, fullMethod string) error {
	if authorization == nil || target.Kind == ProviderKindAuthorization {
		return nil
	}
	subject, err := authorizationSubject(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "provider gateway: %v", err)
	}
	allowed, req, err := runAuthorizationCheck(ctx, authorization, subject, target.Name, fullMethod)
	recordProviderGatewayAuthorizationCheck(ctx, allowed, principal.FromContext(ctx) != nil, req)
	if err != nil {
		return status.Errorf(codes.Internal, "provider gateway: authorize: %v", err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "provider gateway: unauthorized")
	}
	return nil
}
