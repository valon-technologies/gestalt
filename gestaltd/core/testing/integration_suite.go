package coretesting

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
)

// RunIntegrationTests validates an Integration implementation against the
// interface contract. The factory must return a fresh integration configured
// to talk to mockServer for any external HTTP calls.
//
// The mock server must recognize these well-known values:
//   - "valid-code" for ExchangeCode (returns a valid TokenResponse)
//   - "invalid-code" for ExchangeCode (returns an error)
//   - "valid-refresh" for RefreshToken (returns a valid TokenResponse)
//   - "invalid-refresh" for RefreshToken (returns an error)
//   - "valid-bearer-token" for Execute (succeeds)
func RunIntegrationTests(t *testing.T, newIntegration func(t *testing.T, mockURL string) core.OAuthProvider, mockServer *httptest.Server) {
	if mockServer == nil {
		t.Fatal("RunIntegrationTests requires a mock server")
		return
	}
	integration := newIntegration(t, mockServer.URL)

	t.Run("Name", func(t *testing.T) {
		if integration.Name() == "" {
			t.Error("Name() returned empty string")
		}
	})

	t.Run("DisplayName", func(t *testing.T) {
		if integration.DisplayName() == "" {
			t.Error("DisplayName() returned empty string")
		}
	})

	t.Run("Description", func(t *testing.T) {
		if integration.Description() == "" {
			t.Error("Description() returned empty string")
		}
	})

	t.Run("AuthorizationURL", func(t *testing.T) {
		url := integration.AuthorizationURL("state-abc", []string{"read", "write"})
		if url == "" {
			t.Fatal("AuthorizationURL returned empty string")
		}
		if !strings.Contains(url, "state-abc") {
			t.Errorf("AuthorizationURL should contain state parameter; got %q", url)
		}
	})

	t.Run("ExchangeCode", func(t *testing.T) {
		ctx := context.Background()

		resp, err := integration.ExchangeCode(ctx, "valid-code")
		if err != nil {
			t.Fatalf("ExchangeCode(valid-code): %v", err)
		}
		if resp == nil {
			t.Fatal("ExchangeCode returned nil")
			return
		}
		if resp.AccessToken == "" {
			t.Error("AccessToken is empty")
		}
		if resp.TokenType == "" {
			t.Error("TokenType is empty")
		}

		_, err = integration.ExchangeCode(ctx, "invalid-code")
		if err == nil {
			t.Error("ExchangeCode(invalid-code): expected error, got nil")
		}
	})

	t.Run("RefreshToken", func(t *testing.T) {
		ctx := context.Background()

		resp, err := integration.RefreshToken(ctx, "valid-refresh")
		if err != nil {
			t.Fatalf("RefreshToken(valid-refresh): %v", err)
		}
		if resp == nil {
			t.Fatal("RefreshToken returned nil")
			return
		}
		if resp.AccessToken == "" {
			t.Error("AccessToken is empty")
		}

		_, err = integration.RefreshToken(ctx, "invalid-refresh")
		if err == nil {
			t.Error("RefreshToken(invalid-refresh): expected error, got nil")
		}
	})

	t.Run("Catalog", func(t *testing.T) {
		cat := integration.Catalog()
		if cat == nil {
			t.Fatal("Catalog returned nil")
			return
		}
		if len(cat.Operations) == 0 {
			t.Fatal("Catalog returned empty operations")
		}
		for i, op := range cat.Operations {
			if op.ID == "" {
				t.Errorf("operation[%d].ID is empty", i)
			}
			if op.Method == "" {
				t.Errorf("operation[%d].Method is empty", i)
			}
		}
	})

	t.Run("Execute", func(t *testing.T) {
		ctx := context.Background()
		cat := integration.Catalog()
		if cat == nil || len(cat.Operations) == 0 {
			t.Skip("no operations to execute")
			return
		}
		firstOp := firstExecutableOperation(cat)
		if firstOp == nil {
			t.Skip("no executable operations in catalog")
			return
		}

		result, err := integration.Execute(ctx, firstOp.ID, map[string]any{}, "valid-bearer-token")
		if err != nil {
			t.Fatalf("Execute(%q): %v", firstOp.ID, err)
		}
		if result == nil {
			t.Fatal("Execute returned nil")
			return
		}
		if result.Status < 100 || result.Status >= 600 {
			t.Errorf("result.Status: got %d, want valid HTTP status", result.Status)
		}

		_, err = integration.Execute(ctx, "nonexistent-operation", nil, "valid-bearer-token")
		if err == nil {
			t.Error("Execute(nonexistent-operation): expected error, got nil")
		}
	})
}

func firstExecutableOperation(cat *catalog.Catalog) *catalog.CatalogOperation {
	if cat == nil {
		return nil
	}
	for i := range cat.Operations {
		if cat.Operations[i].ID != "" {
			return &cat.Operations[i]
		}
	}
	return nil
}
