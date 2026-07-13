package providergateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type AuthorizationParams struct {
	ProviderID  string
	Operation   string
	CallerToken string
}

func (t *ProviderGatewayTransport) Authorize(ctx context.Context, params AuthorizationParams) (bool, error) {
	if t == nil || t.authorization == nil {
		return true, nil
	}
	allowed, req := t.shadowAuthorizationCheck(ctx, params)
	recordProviderGatewayAuthorizationCheck(ctx, allowed, strings.TrimSpace(params.CallerToken) != "", req)
	return true, nil
}

func (t *ProviderGatewayTransport) shadowAuthorizationCheck(ctx context.Context, params AuthorizationParams) (bool, *proto.CheckAccessRequest) {
	subject, err := t.authorizationSubject(params.CallerToken)
	if err != nil {
		return false, nil
	}
	allowed, req, _ := t.runAuthorizationCheck(ctx, subject, params.ProviderID, params.Operation)
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
	authorization        AuthorizationProvider
	callerTokenPublicKey string
	identity             core.IdentityProvider
	users                UserStore
	publicMethods        *publicrpc.Registry
	publicBaseURL        string
	workflowProviderName string
}

// SetWorkflowProviderName configures the single workflow provider resource
// used by public Workflow RPC authorization. Public callers cannot select this
// value; it is supplied by server configuration.
func (t *ProviderGatewayTransport) SetWorkflowProviderName(name string) {
	if t == nil {
		return
	}
	t.workflowProviderName = strings.TrimSpace(name)
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

func (t *ProviderGatewayTransport) SetCallerTokenPublicKey(publicKey string) {
	if t == nil {
		return
	}
	t.callerTokenPublicKey = strings.TrimSpace(publicKey)
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
		ProviderID:  req.ProviderID,
		Operation:   req.Operation,
		CallerToken: req.CallerToken,
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
	subject *proto.Subject,
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
	req := &proto.CheckAccessRequest{
		Subject:  subject,
		Resource: resource,
		Action:   action,
	}
	resp, err := t.authorization.CheckAccess(ctx, req)
	if err != nil || resp == nil {
		return false, req, err
	}
	return resp.GetAllowed(), req, nil
}

func (t *ProviderGatewayTransport) authorizationSubject(callerToken string) (*proto.Subject, error) {
	if t == nil || strings.TrimSpace(t.callerTokenPublicKey) == "" {
		return nil, fmt.Errorf("provider gateway: caller token public key is required")
	}
	subjectID, err := CallerTokenSubjectID(callerToken, t.callerTokenPublicKey)
	if err != nil {
		return nil, fmt.Errorf("provider gateway: caller token subject: %w", err)
	}
	return &proto.Subject{
		Type: "subject",
		Id:   subjectID,
	}, nil
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
