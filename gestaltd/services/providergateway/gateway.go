package providergateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UserStore interface {
	FindOrCreateUser(ctx context.Context, email string) (*core.User, error)
}

type ProviderGatewayTransport struct {
	authorization core.AuthorizationProvider
	identity      core.IdentityProvider
	users         UserStore
	publicMethods *publicrpc.Registry
	publicBaseURL string
	registry      *LocalRegistry
}

func NewProviderGatewayTransport() *ProviderGatewayTransport {
	return &ProviderGatewayTransport{registry: NewLocalRegistry()}
}

func (t *ProviderGatewayTransport) SetAuthorizationProvider(authorization core.AuthorizationProvider) {
	if t == nil {
		return
	}
	t.authorization = authorization
}

func (t *ProviderGatewayTransport) SetIdentityProvider(identity core.IdentityProvider) {
	if t == nil {
		return
	}
	t.identity = identity
}

func (t *ProviderGatewayTransport) SetUserStore(users UserStore) {
	if t == nil {
		return
	}
	t.users = users
}

func (t *ProviderGatewayTransport) SetPublicMethods(publicMethods *publicrpc.Registry) {
	if t == nil {
		return
	}
	t.publicMethods = publicMethods
}

func (t *ProviderGatewayTransport) SetPublicBaseURL(publicBaseURL string) {
	if t == nil {
		return
	}
	t.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
}

func (t *ProviderGatewayTransport) RegisterDirect(target ProviderTarget, endpoint DirectEndpoint) error {
	if t == nil {
		return fmt.Errorf("provider gateway: transport is nil")
	}
	return t.registry.RegisterDirect(target, endpoint)
}

func (t *ProviderGatewayTransport) ReplaceDirect(target ProviderTarget, endpoint DirectEndpoint) error {
	if t == nil {
		return fmt.Errorf("provider gateway: transport is nil")
	}
	return t.registry.ReplaceDirect(target, endpoint)
}

func (t *ProviderGatewayTransport) Conn(target ProviderTarget) grpc.ClientConnInterface {
	return &targetConn{gateway: t, target: target}
}

type LocalRegistry struct {
	mu     sync.RWMutex
	direct map[ProviderTarget]DirectEndpoint
}

func NewLocalRegistry() *LocalRegistry {
	return &LocalRegistry{direct: make(map[ProviderTarget]DirectEndpoint)}
}

func (r *LocalRegistry) RegisterDirect(target ProviderTarget, endpoint DirectEndpoint) error {
	if endpoint.Conn == nil {
		return fmt.Errorf("provider gateway: direct endpoint for %s/%s requires a connection", target.Kind, target.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.direct[target]; exists {
		return fmt.Errorf("provider gateway: direct endpoint for %s/%s already registered", target.Kind, target.Name)
	}
	r.direct[target] = endpoint
	return nil
}

func (r *LocalRegistry) ReplaceDirect(target ProviderTarget, endpoint DirectEndpoint) error {
	if endpoint.Conn == nil {
		return fmt.Errorf("provider gateway: direct endpoint for %s/%s requires a connection", target.Kind, target.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.direct[target] = endpoint
	return nil
}

func (r *LocalRegistry) Resolve(target ProviderTarget) (*DirectEndpoint, error) {
	r.mu.RLock()
	endpoint, ok := r.direct[target]
	r.mu.RUnlock()
	if !ok {
		return nil, status.Error(codes.NotFound, "provider gateway: route not found")
	}
	return &endpoint, nil
}

type targetConn struct {
	gateway *ProviderGatewayTransport
	target  ProviderTarget
}

func (c *targetConn) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	startedAt := time.Now()
	transportPath := TransportPathUnresolved
	metricReq := metricRequest(c.target, method)

	endpoint, err := c.gateway.registry.Resolve(c.target)
	if err != nil {
		recordProviderGatewayOperation(ctx, startedAt, err, metricReq, transportPath)
		return err
	}
	transportPath = TransportPathDirect
	invokeErr := endpoint.Conn.Invoke(ctx, method, args, reply, opts...)
	recordProviderGatewayOperation(ctx, startedAt, invokeErr, metricReq, transportPath)
	return invokeErr
}

func (c *targetConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	startedAt := time.Now()
	transportPath := TransportPathUnresolved
	metricReq := metricRequest(c.target, method)

	endpoint, err := c.gateway.registry.Resolve(c.target)
	if err != nil {
		recordProviderGatewayOperation(ctx, startedAt, err, metricReq, transportPath)
		return nil, err
	}
	transportPath = TransportPathDirect
	stream, streamErr := endpoint.Conn.NewStream(ctx, desc, method, opts...)
	recordProviderGatewayOperation(ctx, startedAt, streamErr, metricReq, transportPath)
	return stream, streamErr
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

func runAuthorizationCheck(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	subjectID string,
	target ProviderTarget,
) (bool, *proto.CheckAccessRequest, error) {
	if authorization == nil {
		return true, nil, nil
	}
	resource := &proto.Resource{Type: string(target.Kind), Id: strings.TrimSpace(target.Name)}
	action := &proto.Action{Name: strings.TrimSpace(target.Name)}
	req := invocation.SubjectAccessRequest(subjectID, action.GetName(), resource)
	allowed, err := invocation.CheckSubjectAccess(ctx, authorization, req)
	return allowed, req, err
}

func providerKindFromFullMethod(fullMethod string) ProviderKind {
	service, _ := splitFullMethod(fullMethod)
	switch service {
	case proto.App_ServiceDesc.ServiceName:
		return ProviderKindApp
	case proto.Workflow_ServiceDesc.ServiceName:
		return ProviderKindWorkflow
	case proto.Agent_ServiceDesc.ServiceName:
		return ProviderKindAgent
	default:
		return ProviderKind(service)
	}
}
