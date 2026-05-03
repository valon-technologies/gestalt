package coredata

import (
	"context"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

type testExternalCredentialProvider struct{}

func (*testExternalCredentialProvider) PutCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*testExternalCredentialProvider) RestoreCredential(context.Context, *core.ExternalCredential) error {
	return nil
}

func (*testExternalCredentialProvider) GetCredential(context.Context, string, string, string) (*core.ExternalCredential, error) {
	return nil, core.ErrNotFound
}

func (*testExternalCredentialProvider) ListCredentials(context.Context, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*testExternalCredentialProvider) ListCredentialsForConnection(context.Context, string, string) ([]*core.ExternalCredential, error) {
	return nil, nil
}

func (*testExternalCredentialProvider) DeleteCredential(context.Context, string) error {
	return nil
}

func TestEffectiveExternalCredentialProviderHandlesTypedNil(t *testing.T) {
	t.Parallel()

	t.Run("returns configured external credentials", func(t *testing.T) {
		t.Parallel()

		provider := &testExternalCredentialProvider{}
		got := EffectiveExternalCredentialProvider(&Services{
			ExternalCredentials: provider,
		})
		if got != provider {
			t.Fatalf("EffectiveExternalCredentialProvider returned %#v, want %#v", got, provider)
		}
	})

	t.Run("does not wrap typed nil external credentials", func(t *testing.T) {
		t.Parallel()

		var provider *testExternalCredentialProvider
		got := EffectiveExternalCredentialProvider(&Services{ExternalCredentials: provider})
		if got != nil {
			t.Fatalf("EffectiveExternalCredentialProvider returned %#v, want nil", got)
		}
	})
}
