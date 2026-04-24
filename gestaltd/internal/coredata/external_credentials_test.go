package coredata

import "testing"

func TestEffectiveExternalCredentialProviderHandlesTypedNilFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("falls back from typed nil external credentials to tokens", func(t *testing.T) {
		t.Parallel()

		var missing *TokenService
		tokens := &TokenService{}
		got := EffectiveExternalCredentialProvider(&Services{
			ExternalCredentials: missing,
			Tokens:              tokens,
		})
		if got != tokens {
			t.Fatalf("EffectiveExternalCredentialProvider returned %#v, want %#v", got, tokens)
		}
	})

	t.Run("does not wrap typed nil tokens", func(t *testing.T) {
		t.Parallel()

		var tokens *TokenService
		got := EffectiveExternalCredentialProvider(&Services{Tokens: tokens})
		if got != nil {
			t.Fatalf("EffectiveExternalCredentialProvider returned %#v, want nil", got)
		}
	})
}
