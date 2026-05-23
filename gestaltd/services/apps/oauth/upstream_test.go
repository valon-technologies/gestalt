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

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
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

	tests := []struct {
		name      string
		call      func(*UpstreamHandler) (*core.TokenResponse, error)
		wantToken string
	}{
		{
			name: "authorization code",
			call: func(h *UpstreamHandler) (*core.TokenResponse, error) {
				return h.ExchangeCode(context.Background(), "code")
			},
			wantToken: "header-token",
		},
		{
			name: "refresh token",
			call: func(h *UpstreamHandler) (*core.TokenResponse, error) {
				return h.RefreshToken(context.Background(), "rt")
			},
			wantToken: "refreshed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
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
					"access_token": tc.wantToken,
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

			tok, err := tc.call(h)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if tok.AccessToken != tc.wantToken {
				t.Errorf("AccessToken = %q, want %q", tok.AccessToken, tc.wantToken)
			}
			if !strings.HasPrefix(gotAuth, "Basic ") {
				t.Errorf("expected Basic auth header, got %q", gotAuth)
			}
			if formHasClientID {
				t.Error("client_id should not appear in form body with ClientAuthHeader")
			}
		})
	}
}

func TestTokenExchangeJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		call       func(*UpstreamHandler) (*core.TokenResponse, error)
		wantToken  string
		wantGrant  string
		wantField  string
		wantValue  string
		redirect   string
		wantClient bool
	}{
		{
			name: "authorization code",
			call: func(h *UpstreamHandler) (*core.TokenResponse, error) {
				return h.ExchangeCode(context.Background(), "code-123")
			},
			wantToken:  "json-token",
			wantGrant:  "authorization_code",
			wantField:  "code",
			wantValue:  "code-123",
			redirect:   "http://localhost/callback",
			wantClient: true,
		},
		{
			name: "refresh token",
			call: func(h *UpstreamHandler) (*core.TokenResponse, error) {
				return h.RefreshToken(context.Background(), "rt-123")
			},
			wantToken: "json-refreshed",
			wantGrant: "refresh_token",
			wantField: "refresh_token",
			wantValue: "rt-123",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
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
					"access_token": tc.wantToken,
					"token_type":   "Bearer",
				})
			}))
			testutil.CloseOnCleanup(t, srv)

			h := NewUpstream(UpstreamConfig{
				ClientID:      "cid",
				ClientSecret:  "csecret",
				TokenURL:      srv.URL + "/token",
				RedirectURL:   tc.redirect,
				TokenExchange: TokenExchangeJSON,
			})

			tok, err := tc.call(h)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if tok.AccessToken != tc.wantToken {
				t.Errorf("AccessToken = %q, want %q", tok.AccessToken, tc.wantToken)
			}
			if gotContentType != "application/json" {
				t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
			}
			if gotBody["grant_type"] != tc.wantGrant {
				t.Errorf("grant_type = %q, want %q", gotBody["grant_type"], tc.wantGrant)
			}
			if gotBody[tc.wantField] != tc.wantValue {
				t.Errorf("%s = %q, want %q", tc.wantField, gotBody[tc.wantField], tc.wantValue)
			}
			if tc.wantClient && gotBody["client_id"] != "cid" {
				t.Errorf("client_id = %q, want %q", gotBody["client_id"], "cid")
			}
		})
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

func TestAuthorizationURLScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		defaultScopes       []string
		requestScopes       []string
		authorizationParams map[string]string
		wantScope           string
	}{
		{
			name:          "default scopes when caller provides none",
			defaultScopes: []string{"read:data"},
			wantScope:     "read:data",
		},
		{
			name:          "default scopes when caller provides empty",
			defaultScopes: []string{"read", "write"},
			requestScopes: []string{},
			wantScope:     "read write",
		},
		{
			name:          "explicit scopes override defaults",
			defaultScopes: []string{"default-scope"},
			requestScopes: []string{"explicit-scope"},
			wantScope:     "explicit-scope",
		},
		{
			name:                "authorization params override defaults",
			defaultScopes:       []string{"from-spec"},
			authorizationParams: map[string]string{"scope": "from-config"},
			wantScope:           "from-config",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := NewUpstream(UpstreamConfig{
				ClientID:            "client-id",
				AuthorizationURL:    "https://example.com/authorize",
				RedirectURL:         "https://app.com/callback",
				DefaultScopes:       tc.defaultScopes,
				AuthorizationParams: tc.authorizationParams,
			})

			authURL := h.AuthorizationURL("state-1", tc.requestScopes)
			parsed, err := url.Parse(authURL)
			if err != nil {
				t.Fatalf("parsing URL: %v", err)
			}
			scope := parsed.Query().Get("scope")
			if scope != tc.wantScope {
				t.Errorf("scope = %q, want %q", scope, tc.wantScope)
			}
		})
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

func TestWithTokenURLOverride(t *testing.T) {
	t.Parallel()

	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"bearer"}`))
	}))
	t.Cleanup(srv.Close)

	h := NewUpstream(UpstreamConfig{
		ClientID:     "cid",
		ClientSecret: "csec",
		TokenURL:     srv.URL + "/default-token",
		RedirectURL:  "http://localhost/cb",
	})

	_, err := h.ExchangeCode(context.Background(), "code", WithTokenURL(srv.URL+"/override-token"))
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if gotURL != "/override-token" {
		t.Errorf("token request went to %q, want /override-token", gotURL)
	}
}

func TestTokenResponseExtra(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token": "at",
			"refresh_token": "rt",
			"expires_in": 3600,
			"instance_url": "https://na85.salesforce.com",
			"custom_field": "custom_value"
		}`))
	}))
	t.Cleanup(srv.Close)

	h := NewUpstream(UpstreamConfig{
		ClientID:     "cid",
		ClientSecret: "csec",
		TokenURL:     srv.URL + "/token",
		RedirectURL:  "http://localhost/cb",
	})

	resp, err := h.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if resp.AccessToken != "at" {
		t.Errorf("AccessToken = %q, want at", resp.AccessToken)
	}
	if resp.Extra == nil {
		t.Fatal("Extra should not be nil")
	}
	if resp.Extra["instance_url"] != "https://na85.salesforce.com" {
		t.Errorf("Extra[instance_url] = %v, want https://na85.salesforce.com", resp.Extra["instance_url"])
	}
	if resp.Extra["custom_field"] != "custom_value" {
		t.Errorf("Extra[custom_field] = %v, want custom_value", resp.Extra["custom_field"])
	}
}

func TestAccessTokenPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		responseBody    string
		accessTokenPath string
		wantToken       string
	}{
		{
			name: "extracts nested token",
			responseBody: `{
				"ok": true,
				"access_token": "xoxb-bot-token",
				"token_type": "bot",
				"authed_user": {
					"id": "U12345",
					"access_token": "xoxp-user-token",
					"token_type": "user",
					"scope": "chat:write"
				}
			}`,
			accessTokenPath: "authed_user.access_token",
			wantToken:       "xoxp-user-token",
		},
		{
			name:         "falls back to top level",
			responseBody: `{"access_token": "standard-token", "token_type": "Bearer"}`,
			wantToken:    "standard-token",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.responseBody))
			}))
			t.Cleanup(srv.Close)

			h := NewUpstream(UpstreamConfig{
				ClientID:        "cid",
				ClientSecret:    "csec",
				TokenURL:        srv.URL + "/token",
				RedirectURL:     "http://localhost/cb",
				AccessTokenPath: tc.accessTokenPath,
			})

			resp, err := h.ExchangeCode(context.Background(), "code")
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			if resp.AccessToken != tc.wantToken {
				t.Errorf("AccessToken = %q, want %q", resp.AccessToken, tc.wantToken)
			}
		})
	}
}
