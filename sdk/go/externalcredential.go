package gestalt

import externalcredentials "github.com/valon-technologies/gestalt/sdk/go/externalcredentials"

type (
	ExternalCredential                      = externalcredentials.ExternalCredential
	ExternalCredentialLookup                = externalcredentials.ExternalCredentialLookup
	UpsertExternalCredentialRequest         = externalcredentials.UpsertExternalCredentialRequest
	GetExternalCredentialRequest            = externalcredentials.GetExternalCredentialRequest
	ListExternalCredentialsRequest          = externalcredentials.ListExternalCredentialsRequest
	ListExternalCredentialsResponse         = externalcredentials.ListExternalCredentialsResponse
	DeleteExternalCredentialRequest         = externalcredentials.DeleteExternalCredentialRequest
	ExternalCredentialTokenExchangeDriver   = externalcredentials.ExternalCredentialTokenExchangeDriver
	ExternalCredentialAuthConfig            = externalcredentials.ExternalCredentialAuthConfig
	ValidateExternalCredentialConfigRequest = externalcredentials.ValidateExternalCredentialConfigRequest
	ResolveExternalCredentialRequest        = externalcredentials.ResolveExternalCredentialRequest
	ResolveExternalCredentialResponse       = externalcredentials.ResolveExternalCredentialResponse
	ExternalCredentialTokenResponse         = externalcredentials.ExternalCredentialTokenResponse
	ExchangeExternalCredentialRequest       = externalcredentials.ExchangeExternalCredentialRequest
	ExchangeExternalCredentialResponse      = externalcredentials.ExchangeExternalCredentialResponse
)

// ExternalCredentialProvider serves CRUD operations for host-managed external
// credentials.
type ExternalCredentialProvider interface {
	Provider
	externalcredentials.ExternalCredentials
}
