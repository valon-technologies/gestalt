package coretesting

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

// RunAuthProviderTests validates an AuthProvider implementation against the
// interface contract. The factory must return a fresh provider configured to
// talk to mockServer for any external HTTP calls.
//
// The mock server must recognize these well-known values:
//   - "valid-code" for HandleCallback (returns a valid identity)
//   - "invalid-code" for HandleCallback (returns an error)
//   - "valid-token" for ValidateToken (returns a valid identity)
//   - "invalid-token" for ValidateToken (returns an error)
func RunAuthProviderTests(t *testing.T, newProvider func(t *testing.T, mockURL string) core.AuthProvider, mockServer *httptest.Server) {
	if mockServer == nil {
		t.Fatal("RunAuthProviderTests requires a mock server")
		return
	}
	provider := newProvider(t, mockServer.URL)

	t.Run("Name", func(t *testing.T) {
		if provider.Name() == "" {
			t.Error("Name() returned empty string")
		}
	})

	t.Run("LoginURL", func(t *testing.T) {
		url, err := provider.LoginURL("test-state-123")
		if err != nil {
			t.Fatalf("LoginURL: %v", err)
		}
		if url == "" {
			t.Fatal("LoginURL returned empty string")
		}
		if !strings.Contains(url, "test-state-123") {
			t.Errorf("LoginURL should contain state parameter; got %q", url)
		}
	})

	t.Run("HandleCallback", func(t *testing.T) {
		ctx := context.Background()

		identity, err := provider.HandleCallback(ctx, "valid-code")
		if err != nil {
			t.Fatalf("HandleCallback(valid-code): %v", err)
		}
		if identity == nil {
			t.Fatal("HandleCallback returned nil identity")
		}
		if identity.Email == "" {
			t.Error("identity.Email is empty")
		}

		_, err = provider.HandleCallback(ctx, "invalid-code")
		if err == nil {
			t.Error("HandleCallback(invalid-code): expected error, got nil")
		}
	})

	t.Run("ValidateToken", func(t *testing.T) {
		ctx := context.Background()

		identity, err := provider.ValidateToken(ctx, "valid-token")
		if err != nil {
			t.Fatalf("ValidateToken(valid-token): %v", err)
		}
		if identity == nil {
			t.Fatal("ValidateToken returned nil identity")
		}
		if identity.Email == "" {
			t.Error("identity.Email is empty")
		}

		_, err = provider.ValidateToken(ctx, "invalid-token")
		if err == nil {
			t.Error("ValidateToken(invalid-token): expected error, got nil")
		}
	})
}
