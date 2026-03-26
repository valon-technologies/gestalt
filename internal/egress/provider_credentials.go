package egress

import (
	"context"
	"fmt"
	"strings"
)

type CredentialResolver interface {
	ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error)
}

type ProviderTokenResolver interface {
	ResolveProviderToken(ctx context.Context, subject Subject, provider, instance string) (string, error)
}

type CredentialMaterializer interface {
	MaterializeProviderCredential(provider string, token string) (CredentialMaterialization, error)
}

type SecretResolver interface {
	GetSecret(ctx context.Context, name string) (string, error)
}

const secretURIPrefix = "secret://"

func resolveSecretGrant(ctx context.Context, sr SecretResolver, grant *CredentialGrant) (CredentialMaterialization, error) {
	if sr == nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: no secret resolver configured")
	}
	name := strings.TrimPrefix(grant.SecretRef, secretURIPrefix)
	secret, err := sr.GetSecret(ctx, name)
	if err != nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: resolving secret %q: %w", name, err)
	}
	style := grant.AuthStyle
	if style == "" {
		style = AuthStyleBearer
	}
	return MaterializeCredential(secret, style, nil)
}

type CredentialGrant struct {
	Instance  string
	SecretRef string
	AuthStyle AuthStyle
	MatchCriteria
}

func (g *CredentialGrant) ResolveProvider(target Target) (provider, instance string, ok bool) {
	p := g.Provider
	if p == "" {
		p = target.Provider
	}
	if p == "" {
		return "", "", false
	}
	inst := g.Instance
	if inst == "" {
		inst = target.Instance
	}
	if inst == "" {
		inst = p
	}
	return p, inst, true
}

type ProviderCredentialResolver struct {
	TokenResolver  ProviderTokenResolver
	Materializer   CredentialMaterializer
	SecretResolver SecretResolver
	Grants         []CredentialGrant
}

func (r *ProviderCredentialResolver) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	grant, ok := r.matchGrant(subject, target)
	if !ok {
		return CredentialMaterialization{}, nil
	}

	if grant.SecretRef != "" {
		return resolveSecretGrant(ctx, r.SecretResolver, grant)
	}

	if _, canResolve := PrincipalForSubject(subject); !canResolve {
		return CredentialMaterialization{}, nil
	}

	provider, instance, ok := grant.ResolveProvider(target)
	if !ok {
		return CredentialMaterialization{}, nil
	}

	if r.TokenResolver == nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: no token resolver configured")
	}

	token, err := r.TokenResolver.ResolveProviderToken(ctx, subject, provider, instance)
	if err != nil {
		return CredentialMaterialization{}, fmt.Errorf("egress credentials: resolving token for %q: %w", provider, err)
	}

	if r.Materializer == nil {
		return MaterializeCredential(token, AuthStyleBearer, nil)
	}
	return r.Materializer.MaterializeProviderCredential(provider, token)
}

func (r *ProviderCredentialResolver) matchGrant(subject Subject, target Target) (*CredentialGrant, bool) {
	for i := range r.Grants {
		g := &r.Grants[i]
		if !g.Matches(subject, target) {
			continue
		}
		if g.SecretRef != "" {
			return g, true
		}
		if _, _, ok := g.ResolveProvider(target); !ok {
			continue
		}
		return g, true
	}
	return nil, false
}
