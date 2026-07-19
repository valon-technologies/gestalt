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
	invokeErr := invokeDirectUnary(handlerCtx, *endpoint, method, args, reply, opts)
	recordProviderGatewayOperation(ctx, startedAt, invokeErr, metricReq, transportPath)
	return invokeErr
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
}

// NewRoutingGateway returns a ProviderGateway data-plane implementation. A nil
// authorization provider skips the authorization step (optional-provider
// convention); a configured provider is consulted for non-authorization kinds.
func NewRoutingGateway(registry *LocalRegistry, authorization AuthorizationProvider) Gateway {
	if registry == nil {
		panic("provider gateway: local registry is required")
	}
	return &routingGateway{registry: registry, authorization: authorization}
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
