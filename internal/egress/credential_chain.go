package egress

import "context"

type CredentialSourceChain struct {
	Sources []CredentialResolver
}

func (c *CredentialSourceChain) ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error) {
	for _, src := range c.Sources {
		mat, err := src.ResolveCredential(ctx, subject, target)
		if err != nil {
			return CredentialMaterialization{}, err
		}
		if mat.Authorization != "" || len(mat.Headers) > 0 {
			return mat, nil
		}
	}
	return CredentialMaterialization{}, nil
}
