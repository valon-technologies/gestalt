package egress

import "context"

type CredentialResolver interface {
	ResolveCredential(ctx context.Context, subject Subject, target Target) (CredentialMaterialization, error)
}

type SecretResolver interface {
	GetSecret(ctx context.Context, name string) (string, error)
}

type CredentialGrant struct {
	SecretRef string
	AuthStyle AuthStyle
	MatchCriteria
}
