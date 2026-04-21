package invocation

import (
	"context"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type CredentialBindingResolution struct {
	Binding             authorization.CredentialBinding
	HasBinding          bool
	CredentialSubjectID string
	Connection          string
	Instance            string
}

type EffectiveCredentialBindingResolver interface {
	ResolveEffectiveCredentialBinding(p *principal.Principal, providerName, connection, instance string) (CredentialBindingResolution, error)
}

type BindingTokenResolver interface {
	ResolveTokenWithBinding(ctx context.Context, p *principal.Principal, providerName, connection, instance string, boundCredential CredentialBindingResolution) (context.Context, string, error)
}

func ResolveEffectiveCredentialBinding(authz authorization.RuntimeAuthorizer, p *principal.Principal, providerName, connection, instance string) (CredentialBindingResolution, error) {
	return resolveCredentialBinding(authz, p, providerName, connection, instance, false)
}

func ResolveRequestedCredentialBinding(authz authorization.RuntimeAuthorizer, p *principal.Principal, providerName, connection, instance string) (CredentialBindingResolution, error) {
	return resolveCredentialBinding(authz, p, providerName, connection, instance, true)
}

func resolveCredentialBinding(authz authorization.RuntimeAuthorizer, p *principal.Principal, providerName, connection, instance string, enforceRequested bool) (CredentialBindingResolution, error) {
	if authz == nil || p == nil {
		return CredentialBindingResolution{}, nil
	}

	binding, ok := authz.Binding(p, providerName)
	if !ok {
		return CredentialBindingResolution{}, nil
	}

	connection = strings.TrimSpace(connection)
	instance = strings.TrimSpace(instance)
	binding.Mode = core.NormalizeConnectionMode(binding.Mode)

	resolved := CredentialBindingResolution{
		Binding:             binding,
		HasBinding:          true,
		CredentialSubjectID: strings.TrimSpace(binding.CredentialSubjectID),
		Connection:          strings.TrimSpace(binding.Connection),
		Instance:            strings.TrimSpace(binding.Instance),
	}

	if enforceRequested && authz.IsWorkload(p) {
		if connection != "" && connection != resolved.Connection {
			return CredentialBindingResolution{}, bindingSelectorOverrideError()
		}
		if instance != "" && instance != resolved.Instance {
			return CredentialBindingResolution{}, bindingSelectorOverrideError()
		}
	}

	switch binding.Mode {
	case core.ConnectionModeNone:
		return resolved, nil

	case core.ConnectionModeUser:
		if resolved.Connection == "" {
			resolved.Connection = connection
		}

		if resolved.Instance == "" {
			resolved.Instance = instance
		}

		if resolved.CredentialSubjectID == "" {
			resolved.CredentialSubjectID = principal.EffectiveCredentialSubjectID(p)
		}
		return resolved, nil

	default:
		return resolved, nil
	}
}

func bindingSelectorOverrideError() error {
	return fmt.Errorf("%w: callers with bound credentials may not override connection or instance bindings", ErrAuthorizationDenied)
}

func ResolveTokenForBinding(ctx context.Context, resolver TokenResolver, p *principal.Principal, providerName, connection, instance string, boundCredential CredentialBindingResolution) (context.Context, string, error) {
	if resolver == nil {
		return ctx, "", fmt.Errorf("token resolution not supported")
	}
	if boundCredential.HasBinding {
		if bindingResolver, ok := resolver.(BindingTokenResolver); ok {
			return bindingResolver.ResolveTokenWithBinding(ctx, p, providerName, connection, instance, boundCredential)
		}
	}
	return resolver.ResolveToken(ctx, p, providerName, connection, instance)
}
