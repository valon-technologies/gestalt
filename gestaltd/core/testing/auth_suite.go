package coretesting

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

// RunAuthenticationProviderTests validates an AuthenticationProvider
// implementation against the interface contract. The factory must return a
// fresh provider configured to talk to mockServer for any external HTTP calls.
//
// The mock server must recognize these well-known values:
//   - "valid-code" in CompleteAuthenticationRequest.Query["code"] (returns a valid identity)
//   - "invalid-code" in CompleteAuthenticationRequest.Query["code"] (returns an error)
//   - "valid-token" for Authenticate (returns a valid identity)
//   - "invalid-token" for Authenticate (returns an error)
func RunAuthenticationProviderTests(t *testing.T, newProvider func(t *testing.T, mockURL string) core.AuthenticationProvider, mockServer *httptest.Server) {
	if mockServer == nil {
		t.Fatal("RunAuthenticationProviderTests requires a mock server")
		return
	}
	provider := newProvider(t, mockServer.URL)

	t.Run("Name", func(t *testing.T) {
		if provider.Name() == "" {
			t.Error("Name() returned empty string")
		}
	})

	t.Run("BeginAuthentication", func(t *testing.T) {
		resp, err := provider.BeginAuthentication(context.Background(), &core.BeginAuthenticationRequest{
			HostState: "test-state-123",
		})
		if err != nil {
			t.Fatalf("BeginAuthentication: %v", err)
		}
		if resp == nil {
			t.Fatal("BeginAuthentication returned nil response")
		}
		if resp.AuthorizationURL == "" {
			t.Fatal("BeginAuthentication returned empty authorization URL")
		}
		if !strings.Contains(resp.AuthorizationURL, "test-state-123") {
			t.Errorf("BeginAuthentication should contain state parameter; got %q", resp.AuthorizationURL)
		}
	})

	t.Run("CompleteAuthentication", func(t *testing.T) {
		ctx := context.Background()

		identity, err := provider.CompleteAuthentication(ctx, &core.CompleteAuthenticationRequest{
			Query: map[string]string{"code": "valid-code"},
		})
		if err != nil {
			t.Fatalf("CompleteAuthentication(valid-code): %v", err)
		}
		if identity == nil {
			t.Fatal("CompleteAuthentication returned nil identity")
			return
		}
		if identity.Email == "" {
			t.Error("identity.Email is empty")
		}

		_, err = provider.CompleteAuthentication(ctx, &core.CompleteAuthenticationRequest{
			Query: map[string]string{"code": "invalid-code"},
		})
		if err == nil {
			t.Error("CompleteAuthentication(invalid-code): expected error, got nil")
		}
	})

	t.Run("Authenticate", func(t *testing.T) {
		ctx := context.Background()
		authenticator, ok := provider.(core.Authenticator)
		if !ok {
			t.Fatal("provider does not implement core.Authenticator")
		}

		identity, err := authenticator.Authenticate(ctx, &core.AuthenticateRequest{
			Token: &core.TokenAuthInput{Token: "valid-token"},
		})
		if err != nil {
			t.Fatalf("Authenticate(valid-token): %v", err)
		}
		if identity == nil {
			t.Fatal("Authenticate returned nil identity")
			return
		}
		if identity.Email == "" {
			t.Error("identity.Email is empty")
		}

		_, err = authenticator.Authenticate(ctx, &core.AuthenticateRequest{
			Token: &core.TokenAuthInput{Token: "invalid-token"},
		})
		if err == nil {
			t.Error("Authenticate(invalid-token): expected error, got nil")
		}
	})
}
