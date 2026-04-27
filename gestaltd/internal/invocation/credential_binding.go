package invocation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/observability"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"go.opentelemetry.io/otel/attribute"
)

type CredentialBindingResolution struct {
	Binding             authorization.CredentialBinding
	HasBinding          bool
	CredentialSubjectID string
	Connection          string
	Instance            string
}

type EffectiveCredentialBindingResolver interface {
	ResolveEffectiveCredentialBinding(ctx context.Context, p *principal.Principal, providerName, connection, instance string) (CredentialBindingResolution, error)
}

type BindingTokenResolver interface {
	ResolveTokenWithBinding(ctx context.Context, p *principal.Principal, providerName, connection, instance string, boundCredential CredentialBindingResolution) (context.Context, string, error)
}

func ResolveEffectiveCredentialBinding(ctx context.Context, authz authorization.RuntimeAuthorizer, p *principal.Principal, providerName, connection, instance string) (CredentialBindingResolution, error) {
	return resolveCredentialBinding(ctx, authz, p, providerName, connection, instance, false)
}

func ResolveRequestedCredentialBinding(ctx context.Context, authz authorization.RuntimeAuthorizer, p *principal.Principal, providerName, connection, instance string) (CredentialBindingResolution, error) {
	return resolveCredentialBinding(ctx, authz, p, providerName, connection, instance, true)
}

func resolveCredentialBinding(ctx context.Context, authz authorization.RuntimeAuthorizer, p *principal.Principal, providerName, connection, instance string, enforceRequested bool) (result CredentialBindingResolution, err error) {
	startedAt := time.Now()
	mode := "effective"
	if enforceRequested {
		mode = "requested"
	}
	attrs := []attribute.KeyValue{
		attribute.String("gestalt.provider", strings.TrimSpace(providerName)),
		attribute.String("gestalt.credential.binding_mode", mode),
	}
	ctx, span := observability.StartSpan(ctx, "credential.binding.resolve", attrs...)
	defer func() {
		observability.EndSpan(span, err)
		observability.RecordCredentialBindingResolve(ctx, startedAt, err != nil, attrs...)
	}()

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

	if enforceRequested && principal.IsWorkloadPrincipal(p) {
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
	return fmt.Errorf("%w: workloads may not override connection or instance bindings", ErrAuthorizationDenied)
}

func ResolveTokenForBinding(ctx context.Context, resolver TokenResolver, p *principal.Principal, providerName, connection, instance string, boundCredential CredentialBindingResolution) (context.Context, string, error) {
	startedAt := time.Now()
	attrs := []attribute.KeyValue{
		attribute.String("gestalt.provider", strings.TrimSpace(providerName)),
	}
	ctx, span := observability.StartSpan(ctx, "credential.token.resolve", attrs...)
	var err error
	defer func() {
		observability.EndSpan(span, err)
		observability.RecordCredentialTokenResolve(ctx, startedAt, err != nil, attrs...)
	}()

	if resolver == nil {
		err = fmt.Errorf("token resolution not supported")
		return ctx, "", err
	}
	if boundCredential.HasBinding {
		if bindingResolver, ok := resolver.(BindingTokenResolver); ok {
			var token string
			ctx, token, err = bindingResolver.ResolveTokenWithBinding(ctx, p, providerName, connection, instance, boundCredential)
			return ctx, token, err
		}
	}
	var token string
	ctx, token, err = resolver.ResolveToken(ctx, p, providerName, connection, instance)
	return ctx, token, err
}
