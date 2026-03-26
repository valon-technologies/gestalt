package egress

import (
	"context"
	"fmt"
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

type CredentialGrant struct {
	Instance string
	MatchCriteria
}

type ProviderCredentialResolver struct {
	TokenResolver ProviderTokenResolver
	Materializer  CredentialMaterializer
	Grants        []CredentialGrant
}

func (r *ProviderCredentialResolver) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	provider, instance, ok := r.matchGrant(subject, target)
	if !ok {
		return CredentialMaterialization{}, nil
	}

	if _, canResolve := PrincipalForSubject(subject); !canResolve {
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

func (r *ProviderCredentialResolver) matchGrant(subject Subject, target Target) (provider, instance string, matched bool) {
	for i := range r.Grants {
		g := &r.Grants[i]
		if !g.Matches(subject, target) {
			continue
		}
		p := g.Provider
		if p == "" {
			p = target.Provider
		}
		if p == "" {
			continue
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
	return "", "", false
}
