package coredata

import (
	"github.com/valon-technologies/gestalt/server/core"
)

func EffectiveExternalCredentialProvider(services *Services) core.ExternalCredentialProvider {
	if services == nil {
		return nil
	}
	if !ExternalCredentialProviderMissing(services.ExternalCredentials) {
		return services.ExternalCredentials
	}
	return nil
}

func ExternalCredentialProviderMissing(provider core.ExternalCredentialProvider) bool {
	return core.ExternalCredentialProviderMissing(provider)
}
