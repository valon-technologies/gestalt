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
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
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
	if err := authorizeRoutingCall(ctx, c.gateway.authorization, c.gateway.users, c.target, method); err != nil {
		recordProviderGatewayOperation(ctx, startedAt, err, metricReq, transportPath)
		return err
	}
	invokeErr := endpoint.Conn.Invoke(ctx, method, args, reply, opts...)
	recordProviderGatewayOperation(ctx, startedAt, invokeErr, metricReq, transportPath)
	return invokeErr
}

func (c *targetConn) NewStream(ctx context.Context, _ *grpc.StreamDesc, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	startedAt := time.Now()
	err := status.Error(codes.Unimplemented,
		"providergateway: streaming routes land with PG-5 (issue-148); PG-1 is unary-only")
	recordProviderGatewayOperation(ctx, startedAt, err, metricRequest(c.target, method), TransportPathUnresolved)
	return nil, err
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

func authorizeRoutingCall(ctx context.Context, authorization core.AuthorizationProvider, users principal.CredentialUserResolver, target ProviderTarget, fullMethod string) error {
	if authorization == nil || target.Kind == ProviderKindAuthorization {
		return nil
	}
	subjectID, err := authorizationSubject(ctx, users)
	if err != nil {
		return status.Error(codes.Unauthenticated, "provider gateway: caller principal is required")
	}
	allowed, req, checkErr := runAuthorizationCheck(ctx, authorization, subjectID, target.Name, fullMethod)
	recordProviderGatewayAuthorizationCheck(ctx, allowed, principal.FromContext(ctx) != nil, req)
	if checkErr != nil {
		return status.Errorf(codes.Internal, "provider gateway: authorize: %v", checkErr)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "provider gateway: unauthorized")
	}
	return nil
}

func runAuthorizationCheck(
	ctx context.Context,
	authorization core.AuthorizationProvider,
	subjectID, providerID, operation string,
) (bool, *proto.CheckAccessRequest, error) {
	if authorization == nil {
		return true, nil, nil
	}
	resource, err := authorizationResource(providerID, operation)
	if err != nil {
		return false, nil, err
	}
	action, err := authorizationAction(operation)
	if err != nil {
		return false, nil, err
	}
	req := invocation.SubjectAccessRequest(subjectID, action.GetName(), resource)
	allowed, err := invocation.CheckSubjectAccess(ctx, authorization, req)
	return allowed, req, err
}

func authorizationSubject(ctx context.Context, users principal.CredentialUserResolver) (string, error) {
	subjectID, err := principal.ResolveCredentialSubjectID(ctx, users, principal.FromContext(ctx))
	if err != nil {
		return "", fmt.Errorf("provider gateway: caller principal is required")
	}
	return subjectID, nil
}

func authorizationResource(providerID, operation string) (*proto.Resource, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, fmt.Errorf("provider gateway: provider id is required")
	}
	resourceType := "provider"
	if service, _ := splitFullMethod(operation); service == proto.Workflow_ServiceDesc.ServiceName {
		resourceType = "workflow"
	}
	return &proto.Resource{
		Type: resourceType,
		Id:   providerID,
	}, nil
}

func authorizationAction(operation string) (*proto.Action, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return nil, fmt.Errorf("provider gateway: operation is required")
	}
	if service, method := splitFullMethod(operation); service == proto.Workflow_ServiceDesc.ServiceName {
		return &proto.Action{Name: method}, nil
	}
	return &proto.Action{Name: operation}, nil
}
