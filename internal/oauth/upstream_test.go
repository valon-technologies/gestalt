package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/internal/testutil"
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
	testutil.CloseOnCleanup(t, srv)

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
	testutil.CloseOnCleanup(t, srv)

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
	testutil.CloseOnCleanup(t, srv)

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

func TestPKCEGeneration(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "client-id",
		AuthorizationURL: "https://example.com/authorize",
		RedirectURL:      "https://app.com/callback",
		PKCE:             true,
	})

	authURL, verifier := h.AuthorizationURLWithPKCE("state-1", []string{"read"})

	if verifier == "" {
		t.Fatal("expected non-empty verifier when PKCE is enabled")
	}
	if len(verifier) < 43 {
		t.Errorf("verifier length = %d, want >= 43", len(verifier))
	}

	if !strings.Contains(authURL, "code_challenge_method=S256") {
		t.Errorf("URL should contain code_challenge_method=S256; got %q", authURL)
	}
	if !strings.Contains(authURL, "code_challenge=") {
		t.Errorf("URL should contain code_challenge; got %q", authURL)
	}

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	challenge := parsed.Query().Get("code_challenge")

	h256 := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h256[:])
	if challenge != expected {
		t.Errorf("code_challenge = %q, want SHA256 of verifier = %q", challenge, expected)
	}
}

func TestPKCEDisabled(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "client-id",
		AuthorizationURL: "https://example.com/authorize",
		RedirectURL:      "https://app.com/callback",
	})

	authURL, verifier := h.AuthorizationURLWithPKCE("state-1", nil)

	if verifier != "" {
		t.Errorf("expected empty verifier when PKCE is disabled, got %q", verifier)
	}
	if strings.Contains(authURL, "code_challenge") {
		t.Errorf("URL should not contain code_challenge; got %q", authURL)
	}
}

func TestPKCEExchangeCode(t *testing.T) {
	t.Parallel()

	var receivedVerifier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		receivedVerifier = r.FormValue("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "pkce-token",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID: "cid",
		TokenURL: srv.URL + "/token",
		PKCE:     true,
	})

	_, verifier := h.AuthorizationURLWithPKCE("state-1", nil)

	tok, err := h.ExchangeCode(context.Background(), "code", WithPKCEVerifier(verifier))
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "pkce-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "pkce-token")
	}
	if receivedVerifier != verifier {
		t.Errorf("server received verifier %q, want %q", receivedVerifier, verifier)
	}
}

func TestClientAuthHeader(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var formHasClientID bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		formHasClientID = r.FormValue("client_id") != ""
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "header-token",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID:         "my-id",
		ClientSecret:     "my-secret",
		TokenURL:         srv.URL + "/token",
		RedirectURL:      srv.URL + "/callback",
		ClientAuthMethod: ClientAuthHeader,
	})

	tok, err := h.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "header-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "header-token")
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected Basic auth header, got %q", gotAuth)
	}
	if formHasClientID {
		t.Error("client_id should not appear in form body with ClientAuthHeader")
	}
}

func TestClientAuthHeaderRefresh(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var formHasClientID bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		formHasClientID = r.FormValue("client_id") != ""
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID:         "my-id",
		ClientSecret:     "my-secret",
		TokenURL:         srv.URL + "/token",
		ClientAuthMethod: ClientAuthHeader,
	})

	tok, err := h.RefreshToken(context.Background(), "rt")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if tok.AccessToken != "refreshed" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "refreshed")
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected Basic auth header, got %q", gotAuth)
	}
	if formHasClientID {
		t.Error("client_id should not appear in form body with ClientAuthHeader")
	}
}

func TestTokenExchangeJSON(t *testing.T) {
	t.Parallel()

	var gotContentType string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "json-token",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		TokenURL:      srv.URL + "/token",
		RedirectURL:   srv.URL + "/callback",
		TokenExchange: TokenExchangeJSON,
	})

	tok, err := h.ExchangeCode(context.Background(), "code-123")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "json-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "json-token")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
	if gotBody["grant_type"] != "authorization_code" {
		t.Errorf("grant_type = %q, want %q", gotBody["grant_type"], "authorization_code")
	}
	if gotBody["code"] != "code-123" {
		t.Errorf("code = %q, want %q", gotBody["code"], "code-123")
	}
	if gotBody["client_id"] != "cid" {
		t.Errorf("client_id = %q, want %q", gotBody["client_id"], "cid")
	}
}

func TestTokenExchangeJSONRefresh(t *testing.T) {
	t.Parallel()

	var gotContentType string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "json-refreshed",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		TokenURL:      srv.URL + "/token",
		TokenExchange: TokenExchangeJSON,
	})

	tok, err := h.RefreshToken(context.Background(), "rt-123")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if tok.AccessToken != "json-refreshed" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "json-refreshed")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
	if gotBody["grant_type"] != "refresh_token" {
		t.Errorf("grant_type = %q, want %q", gotBody["grant_type"], "refresh_token")
	}
	if gotBody["refresh_token"] != "rt-123" {
		t.Errorf("refresh_token = %q, want %q", gotBody["refresh_token"], "rt-123")
	}
}

func TestTokenExchangeJSONWithClientAuthHeader(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "json-header-token",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID:         "my-id",
		ClientSecret:     "my-secret",
		TokenURL:         srv.URL + "/token",
		RedirectURL:      srv.URL + "/callback",
		TokenExchange:    TokenExchangeJSON,
		ClientAuthMethod: ClientAuthHeader,
	})

	tok, err := h.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "json-header-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "json-header-token")
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected Basic auth header, got %q", gotAuth)
	}
	if _, ok := gotBody["client_id"]; ok {
		t.Error("client_id should not appear in JSON body with ClientAuthHeader")
	}
	if _, ok := gotBody["client_secret"]; ok {
		t.Error("client_secret should not appear in JSON body with ClientAuthHeader")
	}
}

func TestAcceptHeader(t *testing.T) {
	t.Parallel()

	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "accept-token",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	h := NewUpstream(UpstreamConfig{
		ClientID:     "cid",
		ClientSecret: "csecret",
		TokenURL:     srv.URL + "/token",
		RedirectURL:  srv.URL + "/callback",
		AcceptHeader: "application/json",
	})

	tok, err := h.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "accept-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "accept-token")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want %q", gotAccept, "application/json")
	}
}

func TestDefaultFormEncoding(t *testing.T) {
	t.Parallel()

	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "form-token",
			"token_type":   "Bearer",
		})
	}))
	testutil.CloseOnCleanup(t, srv)

	// Empty TokenExchange should default to form encoding.
	h := NewUpstream(UpstreamConfig{
		ClientID: "cid",
		TokenURL: srv.URL + "/token",
	})

	_, err := h.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/x-www-form-urlencoded")
	}
}

func TestGenerateVerifier(t *testing.T) {
	t.Parallel()

	v := GenerateVerifier()
	if len(v) != 43 {
		t.Errorf("verifier length = %d, want 43", len(v))
	}

	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	for _, c := range v {
		if !strings.ContainsRune(charset, c) {
			t.Errorf("verifier contains invalid character %q", c)
		}
	}

	v2 := GenerateVerifier()
	if v == v2 {
		t.Error("two sequential verifiers should differ")
	}
}

func TestDefaultScopesUsedWhenCallerProvidesNone(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "client-id",
		AuthorizationURL: "https://example.com/authorize",
		RedirectURL:      "https://app.com/callback",
		DefaultScopes:    []string{"read:data"},
	})

	authURL := h.AuthorizationURL("state-1", nil)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	scope := parsed.Query().Get("scope")
	if scope != "read:data" {
		t.Errorf("scope = %q, want %q", scope, "read:data")
	}
}

func TestDefaultScopesUsedWhenCallerProvidesEmpty(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "client-id",
		AuthorizationURL: "https://example.com/authorize",
		RedirectURL:      "https://app.com/callback",
		DefaultScopes:    []string{"read", "write"},
	})

	authURL := h.AuthorizationURL("state-1", []string{})
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	scope := parsed.Query().Get("scope")
	if scope != "read write" {
		t.Errorf("scope = %q, want %q", scope, "read write")
	}
}

func TestExplicitScopesOverrideDefaults(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:         "client-id",
		AuthorizationURL: "https://example.com/authorize",
		RedirectURL:      "https://app.com/callback",
		DefaultScopes:    []string{"default-scope"},
	})

	authURL := h.AuthorizationURL("state-1", []string{"explicit-scope"})
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	scope := parsed.Query().Get("scope")
	if scope != "explicit-scope" {
		t.Errorf("scope = %q, want %q", scope, "explicit-scope")
	}
}

func TestAuthorizationParamsOverrideDefaultScopes(t *testing.T) {
	t.Parallel()

	h := NewUpstream(UpstreamConfig{
		ClientID:            "client-id",
		AuthorizationURL:    "https://example.com/authorize",
		RedirectURL:         "https://app.com/callback",
		DefaultScopes:       []string{"from-spec"},
		AuthorizationParams: map[string]string{"scope": "from-config"},
	})

	authURL := h.AuthorizationURL("state-1", nil)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	scope := parsed.Query().Get("scope")
	if scope != "from-config" {
		t.Errorf("scope = %q, want %q (authorization_params should override default scopes)", scope, "from-config")
	}
}

func TestComputeS256Challenge(t *testing.T) {
	t.Parallel()

	// RFC 7636 Appendix B test vector.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := ComputeS256Challenge(verifier)
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	if challenge != expected {
		t.Errorf("challenge = %q, want %q", challenge, expected)
	}
	if strings.Contains(challenge, "=") {
		t.Error("challenge should not contain padding characters")
	}
	if strings.Contains(challenge, "+") || strings.Contains(challenge, "/") {
		t.Error("challenge should use URL-safe base64 encoding")
	}
}
