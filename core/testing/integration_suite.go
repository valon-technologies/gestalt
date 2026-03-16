package coretesting

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/valon-technologies/toolshed/core"
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
func RunIntegrationTests(t *testing.T, newIntegration func(t *testing.T, mockURL string) core.Integration, mockServer *httptest.Server) {
	if mockServer == nil {
		t.Fatal("RunIntegrationTests requires a mock server")
	}
	integration := newIntegration(t, mockServer.URL)

	t.Run("Name", func(t *testing.T) {
		if integration.Name() == "" {
			t.Error("Name() returned empty string")
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
		}
		if resp.AccessToken == "" {
			t.Error("AccessToken is empty")
		}

		_, err = integration.RefreshToken(ctx, "invalid-refresh")
		if err == nil {
			t.Error("RefreshToken(invalid-refresh): expected error, got nil")
		}
	})

	t.Run("ListOperations", func(t *testing.T) {
		ops := integration.ListOperations()
		if len(ops) == 0 {
			t.Fatal("ListOperations returned empty list")
		}
		for i, op := range ops {
			if op.Name == "" {
				t.Errorf("operation[%d].Name is empty", i)
			}
			if op.Method == "" {
				t.Errorf("operation[%d].Method is empty", i)
			}
		}
	})

	t.Run("Execute", func(t *testing.T) {
		ctx := context.Background()
		ops := integration.ListOperations()
		if len(ops) == 0 {
			t.Skip("no operations to execute")
		}

		result, err := integration.Execute(ctx, ops[0].Name, map[string]any{}, "valid-bearer-token")
		if err != nil {
			t.Fatalf("Execute(%q): %v", ops[0].Name, err)
		}
		if result == nil {
			t.Fatal("Execute returned nil")
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
