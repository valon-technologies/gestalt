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
// finding the first matching grant, and materializing it via secret lookup.
// Loaders are evaluated in order; within each loader the first matching grant wins.
type CredentialGrantResolver struct {
	Loaders        []CredentialGrantLoader
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

		mat, err := resolveSecretGrant(ctx, r.SecretResolver, grant)
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
	}
	return nil, false
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
