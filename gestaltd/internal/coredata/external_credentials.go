package coredata

import (
	"reflect"

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
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
