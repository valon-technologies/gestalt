package access

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type Enforcer struct {
	provider core.AuthorizationProvider
}

func NewEnforcer(provider core.AuthorizationProvider) *Enforcer {
	return &Enforcer{provider: provider}
}

func (e *Enforcer) HasProvider() bool {
	return e != nil && e.provider != nil
}

func (e *Enforcer) Allowed(ctx context.Context, p *principal.Principal, req Request) (bool, error) {
	if e == nil || e.provider == nil {
		return true, nil
	}
	if p == nil {
		return false, &accessError{cause: causeNotAuthenticated}
	}
	if req.Subject == nil {
		req.Subject = SubjectFromPrincipal(p)
	}
	if req.Subject == nil {
		return false, &accessError{cause: causeNotAuthenticated}
	}
	resp, err := e.provider.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  req.Subject,
		Action:   req.Action,
		Resource: req.Resource,
	})
	if err != nil {
		return false, &accessError{
			cause:    causePolicyUnavailable,
			resource: resourceName(req.Resource),
			action:   actionName(req.Action),
			err:      err,
		}
	}
	if resp == nil || !resp.Allowed {
		return false, nil
	}
	return true, nil
}

func (e *Enforcer) Require(ctx context.Context, p *principal.Principal, req Request) error {
	allowed, err := e.Allowed(ctx, p, req)
	if err != nil {
		return err
	}
	if !allowed {
		return &accessError{
			cause:    causePolicyDenied,
			resource: resourceName(req.Resource),
			action:   actionName(req.Action),
		}
	}
	return nil
}

func (e *Enforcer) RequireProviderScope(p *principal.Principal, provider string) error {
	provider = strings.TrimSpace(provider)
	if p == nil {
		return &accessError{cause: causeNotAuthenticated, provider: provider}
	}
	if !principal.AllowsProviderPermission(p, provider) {
		return &accessError{cause: causeScopeProvider, provider: provider}
	}
	return nil
}

func (e *Enforcer) RequireOperationScope(p *principal.Principal, provider, operation string) error {
	provider = strings.TrimSpace(provider)
	operation = strings.TrimSpace(operation)
	if p == nil {
		return &accessError{cause: causeNotAuthenticated, provider: provider, operation: operation}
	}
	if !principal.AllowsOperationPermission(p, provider, operation) {
		return &accessError{cause: causeScopeOperation, provider: provider, operation: operation}
	}
	return nil
}

func (e *Enforcer) RequireAppOperation(ctx context.Context, p *principal.Principal, provider, operation string) error {
	if err := e.RequireOperationScope(p, provider, operation); err != nil {
		return err
	}
	return e.Require(ctx, p, AppOperation(provider, operation))
}

func (e *Enforcer) RequireProvider(ctx context.Context, p *principal.Principal, provider string) error {
	if err := e.RequireProviderScope(p, provider); err != nil {
		return err
	}
	return e.Require(ctx, p, Provider(provider))
}

func resourceName(resource *proto.Resource) string {
	if resource == nil {
		return ""
	}
	if name := strings.TrimSpace(resource.Id); name != "" {
		return name
	}
	return strings.TrimSpace(resource.Type)
}

func actionName(action *proto.Action) string {
	if action == nil {
		return ""
	}
	return strings.TrimSpace(action.Name)
}
