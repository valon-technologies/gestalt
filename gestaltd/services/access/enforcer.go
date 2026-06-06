// Package access is the shared gestaltd policy gate.
//
// Require checks one resource/action and optional credential scope. An empty
// action enforces scope only. A nil policy provider means policy checks allow,
// but credential scopes are still enforced.
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

var (
	ErrNotAuthenticated = errors.New("not authenticated")
	ErrDenied           = errors.New("authorization denied")
	ErrScopeDenied      = errors.New("token scope denied")

	errPolicyUnavailable = errors.New("authorization policy unavailable")
)

type CredentialScope uint8

const (
	ProviderCredentialScope CredentialScope = iota + 1
	OperationCredentialScope
)

func Require(ctx context.Context, provider core.AuthorizationProvider, p *principal.Principal, resourceType, action string, scope CredentialScope) error {
	resourceType = strings.TrimSpace(resourceType)
	action = strings.TrimSpace(action)
	switch scope {
	case ProviderCredentialScope:
		if p == nil {
			return withDetails(ErrNotAuthenticated, resourceType, "")
		}
		if !principal.AllowsProviderPermission(p, resourceType) {
			return withDetails(ErrScopeDenied, resourceType, "")
		}
	case OperationCredentialScope:
		if p == nil {
			return withDetails(ErrNotAuthenticated, resourceType, action)
		}
		if !principal.AllowsOperationPermission(p, resourceType, action) {
			return withDetails(ErrScopeDenied, resourceType, action)
		}
	}
	if action == "" {
		return nil
	}
	if provider == nil {
		return nil
	}
	if p == nil {
		return withDetails(ErrNotAuthenticated, resourceType, action)
	}
	subjectID := strings.TrimSpace(principal.EffectiveCredentialSubjectID(p))
	if subjectID == "" {
		return withDetails(ErrNotAuthenticated, resourceType, action)
	}
	resp, err := provider.CheckAccess(ctx, &proto.CheckAccessRequest{
		Subject:  &proto.Subject{Type: "subject", Id: subjectID},
		Action:   &proto.Action{Name: action},
		Resource: &proto.Resource{Type: resourceType, Id: resourceType},
	})
	if err != nil {
		if details := requestDetails(resourceType, action); details != "" {
			return fmt.Errorf("%w: %s: %w", errPolicyUnavailable, details, err)
		}
		return fmt.Errorf("%w: %w", errPolicyUnavailable, err)
	}
	if resp == nil || !resp.Allowed {
		return withDetails(ErrDenied, resourceType, action)
	}
	return nil
}

func IsPolicyUnavailable(err error) bool {
	return errors.Is(err, errPolicyUnavailable)
}

func withDetails(cause error, resource, action string) error {
	if details := requestDetails(resource, action); details != "" {
		return fmt.Errorf("%w: %s", cause, details)
	}
	return cause
}

func requestDetails(resource, action string) string {
	resource = strings.TrimSpace(resource)
	action = strings.TrimSpace(action)
	switch {
	case resource != "" && action != "":
		return resource + " " + action
	case resource != "":
		return resource
	case action != "":
		return action
	default:
		return ""
	}
}
