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
	if provider := LocalExternalCredentialProvider(services); !ExternalCredentialProviderMissing(provider) {
		return provider
	}
	if services.Tokens != nil {
		return services.Tokens
	}
	return nil
}

func LocalExternalCredentialProvider(services *Services) core.ExternalCredentialProvider {
	if services == nil {
		return nil
	}
	if !ExternalCredentialProviderMissing(services.LocalExternalCredentials) {
		return services.LocalExternalCredentials
	}
	if services.Tokens == nil {
		return nil
	}
	return services.Tokens.Provider()
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

func SameExternalCredentialProvider(a, b core.ExternalCredentialProvider) bool {
	if ExternalCredentialProviderMissing(a) || ExternalCredentialProviderMissing(b) {
		return false
	}
	va := reflect.ValueOf(a)
	vb := reflect.ValueOf(b)
	if va.Type() != vb.Type() || !va.Type().Comparable() {
		return false
	}
	return va.Interface() == vb.Interface()
}
