package access

import (
	"context"
	"errors"
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

func OrDefault(e *Enforcer) *Enforcer {
	if e != nil {
		return e
	}
	return NewEnforcer(nil)
}

func (e *Enforcer) HasProvider() bool {
	return e != nil && e.provider != nil
}

func (e *Enforcer) Allowed(ctx context.Context, p *principal.Principal, req Request) (bool, error) {
	if err := scopeError(p, req); err != nil {
		if errors.Is(err, ErrScopeDenied) {
			return false, nil
		}
		return false, err
	}
	return e.policyAllowed(ctx, p, req)
}

func (e *Enforcer) Require(ctx context.Context, p *principal.Principal, req Request) error {
	if err := scopeError(p, req); err != nil {
		return err
	}
	allowed, err := e.policyAllowed(ctx, p, req)
	if err != nil {
		return err
	}
	if !allowed {
		return &accessError{
			cause:    causePolicyDenied,
			resource: resourceName(req.resource()),
			action:   strings.TrimSpace(req.Action),
		}
	}
	return nil
}

func scopeError(p *principal.Principal, req Request) error {
	switch req.CredentialScope {
	case ProviderCredentialScope:
		provider := resourceName(req.resource())
		if p == nil {
			return &accessError{cause: causeNotAuthenticated, provider: provider}
		}
		if !principal.AllowsProviderPermission(p, provider) {
			return &accessError{cause: causeScopeProvider, provider: provider}
		}
	case OperationCredentialScope:
		provider := resourceName(req.resource())
		operation := strings.TrimSpace(req.Action)
		if p == nil {
			return &accessError{cause: causeNotAuthenticated, provider: provider, operation: operation}
		}
		if !principal.AllowsOperationPermission(p, provider, operation) {
			return &accessError{cause: causeScopeOperation, provider: provider, operation: operation}
		}
	}
	return nil
}

func (e *Enforcer) policyAllowed(ctx context.Context, p *principal.Principal, req Request) (bool, error) {
	if req.ScopeOnly {
		return true, nil
	}
	if e == nil || e.provider == nil {
		return true, nil
	}
	if p == nil {
		return false, &accessError{cause: causeNotAuthenticated}
	}
	subject := SubjectFromPrincipal(p)
	if subject == nil {
		return false, &accessError{cause: causeNotAuthenticated}
	}
	resp, err := e.provider.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  subject,
		Action:   req.action(),
		Resource: req.resource(),
	})
	if err != nil {
		return false, &accessError{
			cause:    causePolicyUnavailable,
			resource: resourceName(req.resource()),
			action:   strings.TrimSpace(req.Action),
			err:      err,
		}
	}
	if resp == nil || !resp.Allowed {
		return false, nil
	}
	return true, nil
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
