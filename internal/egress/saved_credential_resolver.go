package egress

import (
	"context"
	"fmt"
)

type SavedCredentialGrantLoader interface {
	ListCandidateCredentialGrants(ctx context.Context, subject Subject, target Target) ([]CredentialGrant, error)
}

type SavedGrantCredentialResolver struct {
	Store          SavedCredentialGrantLoader
	TokenResolver  ProviderTokenResolver
	Materializer   CredentialMaterializer
	SecretResolver SecretResolver
}

func (r *SavedGrantCredentialResolver) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	candidates, err := r.Store.ListCandidateCredentialGrants(ctx, subject, target)
	if err != nil {
		return CredentialMaterialization{}, fmt.Errorf("egress saved grants: loading candidates: %w", err)
	}

	for i := range candidates {
		g := &candidates[i]
		if !g.Matches(subject, target) {
			continue
		}

		if g.SecretRef != "" {
			return resolveSecretGrant(ctx, r.SecretResolver, g)
		}

		provider, instance, ok := g.ResolveProvider(target)
		if !ok {
			continue
		}

		if _, canResolve := PrincipalForSubject(subject); !canResolve {
			return CredentialMaterialization{}, nil
		}

		if r.TokenResolver == nil {
			return CredentialMaterialization{}, fmt.Errorf("egress saved grants: no token resolver configured")
		}

		token, err := r.TokenResolver.ResolveProviderToken(ctx, subject, provider, instance)
		if err != nil {
			return CredentialMaterialization{}, fmt.Errorf("egress saved grants: resolving token for %q: %w", provider, err)
		}

		if r.Materializer == nil {
			return MaterializeCredential(token, AuthStyleBearer, nil)
		}
		return r.Materializer.MaterializeProviderCredential(provider, token)
	}

	return CredentialMaterialization{}, nil
}
