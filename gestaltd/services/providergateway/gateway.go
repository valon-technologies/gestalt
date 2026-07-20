package providergateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type AuthorizationParams struct {
	ProviderID string
	Operation  string
}

func (t *ProviderGatewayTransport) Authorize(ctx context.Context, params AuthorizationParams) (bool, error) {
	if t == nil || t.authorization == nil {
		return true, nil
	}
	allowed, req := t.shadowAuthorizationCheck(ctx, params)
	recordProviderGatewayAuthorizationCheck(ctx, allowed, principal.FromContext(ctx) != nil, req)
	return allowed, nil
}

func (t *ProviderGatewayTransport) shadowAuthorizationCheck(ctx context.Context, params AuthorizationParams) (bool, *proto.CheckAccessRequest) {
	subjectID, err := t.authorizationSubject(ctx)
	if err != nil {
		return false, nil
	}
	allowed, req, _ := t.runAuthorizationCheck(ctx, subjectID, params.ProviderID, params.Operation)
	return allowed, req
}

type DirectTransport struct{}

func (DirectTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (resp ProviderGatewayResponse, err error) {
	startedAt := time.Now()
	defer func() {
		recordProviderGatewayOperation(ctx, startedAt, err, req, TransportPathDirect)
	}()

	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	return next(ctx, req)
}

type UserStore interface {
	FindOrCreateUser(ctx context.Context, email string) (*core.User, error)
}

type ProviderGatewayTransport struct {
	authorization AuthorizationProvider
	identity      core.IdentityProvider
	users         UserStore
	publicMethods *publicrpc.Registry
	publicBaseURL string
}

func NewProviderGatewayTransport() *ProviderGatewayTransport {
	return &ProviderGatewayTransport{}
}

func (t *ProviderGatewayTransport) SetAuthorizationProvider(authorization AuthorizationProvider) {
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

func (t *ProviderGatewayTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (resp ProviderGatewayResponse, err error) {
	startedAt := time.Now()
	defer func() {
		recordProviderGatewayOperation(ctx, startedAt, err, req, TransportPathProviderGateway)
	}()

	if t == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: transport is nil")
	}
	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	allowed, err := t.Authorize(ctx, AuthorizationParams{
		ProviderID: req.ProviderID,
		Operation:  req.Operation,
	})
	if err != nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: authorize: %w", err)
	}
	if !allowed {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: unauthorized")
	}
	return next(ctx, req)
}

func (t *ProviderGatewayTransport) runAuthorizationCheck(
	ctx context.Context,
	subjectID string,
	providerID, operation string,
) (bool, *proto.CheckAccessRequest, error) {
	if t == nil || t.authorization == nil {
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
	allowed, err := invocation.CheckSubjectAccess(ctx, t.authorization.(core.AuthorizationProvider), req)
	return allowed, req, err
}

func (t *ProviderGatewayTransport) authorizationSubject(ctx context.Context) (string, error) {
	subjectID, err := principal.ResolveCredentialSubjectID(ctx, t.users, principal.FromContext(ctx))
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
