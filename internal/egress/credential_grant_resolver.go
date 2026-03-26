package egress

import (
	"context"
	"fmt"
	"strings"
)

const secretURIPrefix = "secret://"

// CredentialGrantLoader loads credential grants for resolution. Implementations
// may return static config grants or fetch persisted grants from a store.
type CredentialGrantLoader interface {
	LoadCredentialGrants(ctx context.Context) ([]CredentialGrant, error)
}

// StaticCredentialGrantLoader wraps a fixed set of grants, typically from config.
type StaticCredentialGrantLoader struct {
	Grants []CredentialGrant
}

func (l *StaticCredentialGrantLoader) LoadCredentialGrants(_ context.Context) ([]CredentialGrant, error) {
	return l.Grants, nil
}

// CredentialGrantResolver resolves credentials by iterating ordered loaders,
// finding the first matching grant, and materializing it. Loaders are evaluated
// in order; within each loader the first matching grant wins.
type CredentialGrantResolver struct {
	Loaders        []CredentialGrantLoader
	TokenResolver  ProviderTokenResolver
	Materializer   CredentialMaterializer
	SecretResolver SecretResolver
}

func (r *CredentialGrantResolver) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	for _, loader := range r.Loaders {
		grants, err := loader.LoadCredentialGrants(ctx)
		if err != nil {
			return CredentialMaterialization{}, fmt.Errorf("egress credentials: loading grants: %w", err)
		}

		grant, ok := firstMatchingGrant(grants, subject, target)
		if !ok {
			continue
		}

		mat, err := r.resolveGrantCredential(ctx, subject, target, grant)
		if err != nil {
			return CredentialMaterialization{}, err
		}
		if mat.Authorization != "" || len(mat.Headers) > 0 {
			return mat, nil
		}
	}
	return CredentialMaterialization{}, nil
}

func firstMatchingGrant(grants []CredentialGrant, subject Subject, target Target) (*CredentialGrant, bool) {
	for i := range grants {
		g := &grants[i]
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

func (r *CredentialGrantResolver) resolveGrantCredential(ctx context.Context, subject Subject, target Target, grant *CredentialGrant) (CredentialMaterialization, error) {
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
