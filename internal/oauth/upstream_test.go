package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorizationURL(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "client-id",
		ClientSecret:     "client-secret",
		AuthorizationURL: "https://example.com/authorize",
		TokenURL:         "https://example.com/token",
		RedirectURL:      "https://app.com/callback",
	})

	u := h.AuthorizationURL("state-123", []string{"read", "write"})
	if !strings.Contains(u, "state-123") {
		t.Errorf("URL should contain state; got %q", u)
	}
	if !strings.Contains(u, "client_id=client-id") {
		t.Errorf("URL should contain client_id; got %q", u)
	}
	if !strings.Contains(u, "scope=read+write") {
		t.Errorf("URL should contain scopes; got %q", u)
	}
	if !strings.Contains(u, "response_type=code") {
		t.Errorf("URL should contain response_type=code; got %q", u)
	}
}

func TestExchangeCode(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		code := r.FormValue("code")
		if code == "valid-code" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "test-access-token",
				"refresh_token": "test-refresh-token",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	defer srv.Close()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "cid",
		ClientSecret:     "csecret",
		AuthorizationURL: srv.URL + "/authorize",
		TokenURL:         srv.URL + "/token",
		RedirectURL:      srv.URL + "/callback",
	})

	ctx := context.Background()

	tok, err := h.ExchangeCode(ctx, "valid-code")
	if err != nil {
		t.Fatalf("ExchangeCode(valid-code): %v", err)
	}
	if tok.AccessToken != "test-access-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "test-access-token")
	}
	if tok.RefreshToken != "test-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", tok.RefreshToken, "test-refresh-token")
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want %q", tok.TokenType, "Bearer")
	}

	_, err = h.ExchangeCode(ctx, "invalid-code")
	if err == nil {
		t.Fatal("ExchangeCode(invalid-code): expected error")
	}
}

func TestRefreshToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		refresh := r.FormValue("refresh_token")
		if refresh == "valid-refresh" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "refreshed-token",
				"token_type":   "Bearer",
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	defer srv.Close()

	h := NewUpstream(UpstreamConfig{
		ClientID:     "cid",
		ClientSecret: "csecret",
		TokenURL:     srv.URL + "/token",
	})

	ctx := context.Background()

	tok, err := h.RefreshToken(ctx, "valid-refresh")
	if err != nil {
		t.Fatalf("RefreshToken(valid-refresh): %v", err)
	}
	if tok.AccessToken != "refreshed-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "refreshed-token")
	}

	_, err = h.RefreshToken(ctx, "invalid-refresh")
	if err == nil {
		t.Fatal("RefreshToken(invalid-refresh): expected error")
	}
}

func TestResponseHook(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           false,
			"error":        "custom_error",
			"access_token": "tok",
			"token_type":   "Bearer",
		})
	}))
	defer srv.Close()

	hookCalled := false
	h := NewUpstream(
		UpstreamConfig{TokenURL: srv.URL + "/token"},
		WithResponseHook(func(body []byte) error {
			hookCalled = true
			var resp struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("provider error: %s", resp.Error)
			}
			return nil
		}),
	)

	_, err := h.ExchangeCode(context.Background(), "any")
	if !hookCalled {
		t.Fatal("response hook was not called")
	}
	if err == nil {
		t.Fatal("expected error from response hook")
	}
}
