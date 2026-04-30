package providerhost

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	externalcredentialsservice "github.com/valon-technologies/gestalt/server/services/externalcredentials"
)

const DefaultExternalCredentialSocketEnv = externalcredentialsservice.DefaultSocketEnv

func NewExternalCredentialProviderServer(provider core.ExternalCredentialProvider) proto.ExternalCredentialProviderServer {
	return externalcredentialsservice.NewProviderServer(provider)
}
