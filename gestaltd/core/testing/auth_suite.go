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
//   - "valid-code" for Token authorization_code exchange (returns an active access token)
//   - "invalid-code" for Token (returns an error)
//   - "valid-token" for Introspect (returns active=true)
//   - "invalid-token" for Introspect (returns active=false)
func RunAuthenticationProviderTests(t *testing.T, newProvider func(t *testing.T, mockURL string) core.AuthenticationProvider, mockServer *httptest.Server) {
	if mockServer == nil {
		t.Fatal("RunAuthenticationProviderTests requires a mock server")
		return
	}
	provider := newProvider(t, mockServer.URL)
	ctx := context.Background()

	t.Run("Authorize", func(t *testing.T) {
		resp, err := provider.Authorize(ctx, &core.AuthorizeRequest{
			ResponseType: "code",
			ClientID:     core.DefaultOAuthClientID,
			RedirectURI:  mockServer.URL + "/callback",
			State:        "test-state-123",
		})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if resp == nil || resp.RedirectURI == "" {
			t.Fatal("Authorize returned empty redirect URI")
		}
		if !strings.Contains(resp.RedirectURI, "test-state-123") {
			t.Errorf("Authorize redirect should contain state parameter; got %q", resp.RedirectURI)
		}
	})

	t.Run("Token", func(t *testing.T) {
		resp, err := provider.Token(ctx, &core.TokenRequest{
			GrantType:   "authorization_code",
			Code:        "valid-code",
			RedirectURI: mockServer.URL + "/callback",
			ClientID:    core.DefaultOAuthClientID,
		})
		if err != nil {
			t.Fatalf("Token(valid-code): %v", err)
		}
		if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
			t.Fatal("Token returned empty access token")
		}

		_, err = provider.Token(ctx, &core.TokenRequest{
			GrantType:   "authorization_code",
			Code:        "invalid-code",
			RedirectURI: mockServer.URL + "/callback",
			ClientID:    core.DefaultOAuthClientID,
		})
		if err == nil {
			t.Error("Token(invalid-code): expected error, got nil")
		}
	})

	t.Run("Introspect", func(t *testing.T) {
		resp, err := provider.Introspect(ctx, &core.IntrospectRequest{Token: "valid-token"})
		if err != nil {
			t.Fatalf("Introspect(valid-token): %v", err)
		}
		if resp == nil || !resp.Active {
			t.Fatal("Introspect(valid-token) expected active token")
		}
		if strings.TrimSpace(resp.Subject) == "" {
			t.Error("Introspect subject is empty")
		}

		resp, err = provider.Introspect(ctx, &core.IntrospectRequest{Token: "invalid-token"})
		if err != nil {
			t.Fatalf("Introspect(invalid-token): %v", err)
		}
		if resp != nil && resp.Active {
			t.Error("Introspect(invalid-token): expected inactive token")
		}
	})
}
