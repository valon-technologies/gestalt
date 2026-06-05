package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type Enforcer struct {
	provider core.AuthorizationProvider
}

func NewEnforcer(provider core.AuthorizationProvider) *Enforcer {
	if provider == nil {
		return nil
	}
	return &Enforcer{provider: provider}
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
		return deniedError(ErrDenied, resourceName(req.resource()), req.Action)
	}
	return nil
}

func scopeError(p *principal.Principal, req Request) error {
	switch req.CredentialScope {
	case ProviderCredentialScope:
		provider := resourceName(req.resource())
		if p == nil {
			return deniedError(ErrNotAuthenticated, provider, "")
		}
		if !principal.AllowsProviderPermission(p, provider) {
			return deniedError(ErrScopeDenied, provider, "")
		}
	case OperationCredentialScope:
		provider := resourceName(req.resource())
		operation := strings.TrimSpace(req.Action)
		if p == nil {
			return deniedError(ErrNotAuthenticated, provider, operation)
		}
		if !principal.AllowsOperationPermission(p, provider, operation) {
			return deniedError(ErrScopeDenied, provider, operation)
		}
	}
	return nil
}

func (e *Enforcer) policyAllowed(ctx context.Context, p *principal.Principal, req Request) (bool, error) {
	action := strings.TrimSpace(req.Action)
	if action == "" {
		return true, nil
	}
	if e == nil || e.provider == nil {
		return true, nil
	}
	if p == nil {
		return false, ErrNotAuthenticated
	}
	subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
	if subjectID == "" {
		return false, ErrNotAuthenticated
	}
	resource := req.resource()
	resp, err := e.provider.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: subjectID},
		Action:   &proto.Action{Name: action},
		Resource: resource,
	})
	if err != nil {
		if details := accessDetails(resourceName(resource), action); details != "" {
			return false, fmt.Errorf("%w: %s: %w", errPolicyUnavailable, details, err)
		}
		return false, fmt.Errorf("%w: %w", errPolicyUnavailable, err)
	}
	if resp == nil || !resp.Allowed {
		return false, nil
	}
	return true, nil
}

func deniedError(cause error, resource, action string) error {
	if details := accessDetails(resource, action); details != "" {
		return fmt.Errorf("%w: %s", cause, details)
	}
	return cause
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
