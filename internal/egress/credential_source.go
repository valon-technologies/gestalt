package egress

import (
	"context"
	"fmt"
	"strings"
)

const secretURIPrefix = "secret://"

// CredentialSource resolves credentials for a matched grant. Implementations
// short-circuit internally before any I/O when they cannot serve a request.
// An empty CredentialMaterialization (not an error) signals "cannot resolve"
// and allows the resolver to fall through to the next grant or loader.
type CredentialSource interface {
	ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error)
}

// SecretCredentialSource resolves credentials by fetching a named secret.
// Works for any subject kind.
type SecretCredentialSource struct {
	Resolver  SecretResolver
	SecretRef string
	AuthStyle AuthStyle
}

func (s *SecretCredentialSource) ResolveCredential(ctx context.Context, _ Subject, _ Target) (CredentialMaterialization, error) {
	if s.Resolver == nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: no secret resolver configured")
	}
	name := strings.TrimPrefix(s.SecretRef, secretURIPrefix)
	secret, err := s.Resolver.GetSecret(ctx, name)
	if err != nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: resolving secret %q: %w", name, err)
	}
	style := s.AuthStyle
	if style == "" {
		style = AuthStyleBearer
	}
	return MaterializeCredential(secret, style, nil)
}

// ProviderTokenCredentialSource resolves credentials via a provider's token
// broker and materializer. Only works for principal subjects (user, identity,
// agent). Returns empty for non-principal subjects or when provider/instance
// cannot be derived from the target.
type ProviderTokenCredentialSource struct {
	TokenResolver ProviderTokenResolver
	Materializer  CredentialMaterializer
	Provider      string
	Instance      string
}

func (s *ProviderTokenCredentialSource) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	if _, ok := PrincipalForSubject(subject); !ok {
		return CredentialMaterialization{}, nil
	}

	provider, instance, ok := s.resolveProvider(target)
	if !ok {
		return CredentialMaterialization{}, nil
	}

	if s.TokenResolver == nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: no token resolver configured")
	}

	token, err := s.TokenResolver.ResolveProviderToken(ctx, subject, provider, instance)
	if err != nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: resolving token for %q: %w", provider, err)
	}

	if s.Materializer == nil {
		return MaterializeCredential(token, AuthStyleBearer, nil)
	}
	return s.Materializer.MaterializeProviderCredential(provider, token)
}

func (s *ProviderTokenCredentialSource) resolveProvider(target Target) (provider, instance string, ok bool) {
	p := s.Provider
	if p == "" {
		p = target.Provider
	}
	if p == "" {
		return "", "", false
	}
	inst := s.Instance
	if inst == "" {
		inst = target.Instance
	}
	if inst == "" {
		inst = p
	}
	return p, inst, true
}
