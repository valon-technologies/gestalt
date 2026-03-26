package egress

import (
	"context"
	"fmt"
)

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
// finding the first matching grant whose source produces a non-empty result.
// Loaders are evaluated in order; within each loader the first matching grant wins.
type CredentialGrantResolver struct {
	Loaders []CredentialGrantLoader
}

func (r *CredentialGrantResolver) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	for _, loader := range r.Loaders {
		grants, err := loader.LoadCredentialGrants(ctx)
		if err != nil {
			return CredentialMaterialization{}, fmt.Errorf("egress credentials: loading grants: %w", err)
		}

		for i := range grants {
			g := &grants[i]
			if !g.Matches(subject, target) || g.Source == nil {
				continue
			}
			mat, err := g.Source.ResolveCredential(ctx, subject, target)
			if err != nil {
				return CredentialMaterialization{}, err
			}
			if mat.Authorization != "" || len(mat.Headers) > 0 {
				return mat, nil
			}
		}
	}
	return CredentialMaterialization{}, nil
}
