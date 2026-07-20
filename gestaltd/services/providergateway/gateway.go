package providergateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/publicrpc"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
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
}

func NewProviderGatewayTransport() *ProviderGatewayTransport {
	return &ProviderGatewayTransport{}
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
	allowed, err := invocation.CheckSubjectAccess(ctx, t.authorization, req)
	return allowed, req, err
}

func (t *ProviderGatewayTransport) authorizationSubject(ctx context.Context) (string, error) {
	subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(principal.FromContext(ctx)))
	if subjectID == "" {
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
