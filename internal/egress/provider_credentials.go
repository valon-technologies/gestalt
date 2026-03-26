package egress

import "context"

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
