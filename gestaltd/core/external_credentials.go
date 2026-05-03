package core

import (
	"context"
	"reflect"
)

// ExternalCredentialProvider manages subject-scoped third-party credentials
// used to invoke integrations on behalf of users or other canonical subjects.
// subjects.
type ExternalCredentialProvider interface {
	PutCredential(ctx context.Context, credential *ExternalCredential) error
	RestoreCredential(ctx context.Context, credential *ExternalCredential) error
	GetCredential(ctx context.Context, subjectID, connectionID, instance string) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, subjectID string) ([]*ExternalCredential, error)
	ListCredentialsForConnection(ctx context.Context, subjectID, connectionID string) ([]*ExternalCredential, error)
	DeleteCredential(ctx context.Context, id string) error
}

// ExternalCredentialProviderMissing reports whether provider is nil, including
// typed nil implementations stored in the interface.
func ExternalCredentialProviderMissing(provider ExternalCredentialProvider) bool {
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
