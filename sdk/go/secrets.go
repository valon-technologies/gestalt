package gestalt

import secretsapi "github.com/valon-technologies/gestalt/sdk/go/secrets"

// SecretsProvider serves secret lookups for providers that need host-managed
// secret material.
type SecretsProvider interface {
	Provider
	secretsapi.Secrets
}
