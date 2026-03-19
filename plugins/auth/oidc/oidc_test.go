package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"github.com/valon-technologies/toolshed/internal/oauth"
	"golang.org/x/oauth2"
)

func newMockOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DiscoveryDocument{
			Issuer:                server.URL,
			AuthorizationEndpoint: server.URL + "/authorize",
			TokenEndpoint:         server.URL + "/token",
			UserinfoEndpoint:      server.URL + "/userinfo",
			JwksURI:               server.URL + "/jwks",
			ScopesSupported:       []string{"openid", "email", "profile"},
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		code := r.FormValue("code")
		switch code {
		case "valid-code":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "mock-access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "mock-refresh-token",
			})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_grant",
				"error_description": "invalid authorization code",
			})
		}
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")

		switch token {
		case "mock-access-token", "valid-token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"email":          "user@example.com",
				"name":           "Test User",
				"picture":        "https://example.com/avatar.png",
				"email_verified": true,
			})
		case "unverified-token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"email":          "unverified@example.com",
				"name":           "Unverified User",
				"email_verified": false,
			})
		default:
			http.Error(w, "invalid token", http.StatusUnauthorized)
		}
	})

	server = httptest.NewServer(mux)
	return server
}

func newTestProvider(t *testing.T, mockURL string, opts ...func(*Provider)) *Provider {
	t.Helper()

	doc := &DiscoveryDocument{
		Issuer:                mockURL,
		AuthorizationEndpoint: mockURL + "/authorize",
		TokenEndpoint:         mockURL + "/token",
		UserinfoEndpoint:      mockURL + "/userinfo",
		JwksURI:               mockURL + "/jwks",
		ScopesSupported:       []string{"openid", "email", "profile"},
	}

	p := &Provider{
		oauth2Cfg: &oauth2.Config{
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			RedirectURL:  "http://localhost/callback",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  mockURL + "/authorize",
				TokenURL: mockURL + "/token",
			},
		},
		discovery:   doc,
		httpClient:  &http.Client{},
		allowed:     make(map[string]bool),
		secret:      []byte("test-secret-key-32-bytes-long!!!"),
		ttl:         time.Hour,
		displayName: "SSO",
	}

	for _, fn := range opts {
		fn(p)
	}
	return p
}

func newDomainRestrictedProvider(t *testing.T, mockURL string, domains []string) *Provider {
	t.Helper()
	allowed := make(map[string]bool, len(domains))
	for _, d := range domains {
		allowed[strings.ToLower(d)] = true
	}
	return newTestProvider(t, mockURL, func(p *Provider) {
		p.allowed = allowed
	})
}

func TestOIDCAuthConformance(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	coretesting.RunAuthProviderTests(t, func(t *testing.T, mockURL string) core.AuthProvider {
		return newTestProvider(t, mockURL)
	}, mockServer)
}

func TestDiscovery(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	doc, err := Discover(context.Background(), mockServer.URL, &http.Client{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if doc.Issuer != mockServer.URL {
		t.Errorf("issuer = %q, want %q", doc.Issuer, mockServer.URL)
	}
	if doc.AuthorizationEndpoint != mockServer.URL+"/authorize" {
		t.Errorf("authorization_endpoint = %q", doc.AuthorizationEndpoint)
	}
	if doc.TokenEndpoint != mockServer.URL+"/token" {
		t.Errorf("token_endpoint = %q", doc.TokenEndpoint)
	}
	if doc.UserinfoEndpoint != mockServer.URL+"/userinfo" {
		t.Errorf("userinfo_endpoint = %q", doc.UserinfoEndpoint)
	}
}

func TestDiscoveryIssuerMismatch(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DiscoveryDocument{
			Issuer:                "https://wrong-issuer.example.com",
			AuthorizationEndpoint: "https://wrong-issuer.example.com/authorize",
			TokenEndpoint:         "https://wrong-issuer.example.com/token",
			UserinfoEndpoint:      "https://wrong-issuer.example.com/userinfo",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := Discover(context.Background(), server.URL, &http.Client{})
	if err == nil {
		t.Fatal("expected error for issuer mismatch")
	}
	if !strings.Contains(err.Error(), "issuer mismatch") {
		t.Errorf("error should mention issuer mismatch: %v", err)
	}
}

func TestLoginURL(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)
	loginURL, err := p.LoginURL("my-state")
	if err != nil {
		t.Fatalf("LoginURL: %v", err)
	}

	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("parse LoginURL: %v", err)
	}

	if !strings.Contains(loginURL, "my-state") {
		t.Errorf("LoginURL missing state: %s", loginURL)
	}
	if parsed.Query().Get("client_id") != "test-client-id" {
		t.Errorf("LoginURL missing client_id: %s", loginURL)
	}
	if parsed.Query().Get("redirect_uri") != "http://localhost/callback" {
		t.Errorf("LoginURL missing redirect_uri: %s", loginURL)
	}
	if parsed.Query().Get("code_challenge") != "" {
		t.Error("non-PKCE LoginURL should not have code_challenge")
	}
}

func TestLoginURLWithPKCE(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.pkce = true
	})

	loginURL, err := p.LoginURL("my-state")
	if err != nil {
		t.Fatalf("LoginURL: %v", err)
	}

	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("parse LoginURL: %v", err)
	}

	if parsed.Query().Get("code_challenge") == "" {
		t.Error("PKCE LoginURL should have code_challenge")
	}
	if parsed.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", parsed.Query().Get("code_challenge_method"))
	}

	encodedState := parsed.Query().Get("state")
	origState, verifier, err := p.decodePKCEState(encodedState)
	if err != nil {
		t.Fatalf("decodePKCEState: %v", err)
	}
	if origState != "my-state" {
		t.Errorf("decoded state = %q, want %q", origState, "my-state")
	}
	if verifier == "" {
		t.Error("decoded verifier is empty")
	}

	expectedChallenge := oauth.ComputeS256Challenge(verifier)
	if parsed.Query().Get("code_challenge") != expectedChallenge {
		t.Errorf("code_challenge mismatch: got %q, want %q",
			parsed.Query().Get("code_challenge"), expectedChallenge)
	}
}

func TestHandleCallback(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)

	identity, err := p.HandleCallback(context.Background(), "valid-code")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if identity.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", identity.Email, "user@example.com")
	}
	if identity.DisplayName != "Test User" {
		t.Errorf("display_name = %q, want %q", identity.DisplayName, "Test User")
	}
	if identity.AvatarURL != "https://example.com/avatar.png" {
		t.Errorf("avatar_url = %q, want %q", identity.AvatarURL, "https://example.com/avatar.png")
	}
}

func TestHandleCallbackRejectsWhenPKCEEnabled(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.pkce = true
	})

	_, err := p.HandleCallback(context.Background(), "valid-code")
	if err == nil {
		t.Fatal("expected error when calling HandleCallback with PKCE enabled")
	}
	if !strings.Contains(err.Error(), "HandleCallbackWithState") {
		t.Errorf("error should mention HandleCallbackWithState: %v", err)
	}
}

func TestHandleCallbackDomainRestriction(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newDomainRestrictedProvider(t, mockServer.URL, []string{"allowed.com"})

	_, err := p.HandleCallback(context.Background(), "valid-code")
	if err == nil {
		t.Fatal("expected error for disallowed domain")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error should mention domain restriction: %v", err)
	}
}

func TestUnverifiedEmailRejected(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)

	_, err := p.fetchUserInfo(context.Background(), "unverified-token")
	if err == nil {
		t.Fatal("expected error for unverified email")
	}
	if !strings.Contains(err.Error(), "not verified") {
		t.Errorf("error should mention verification: %v", err)
	}
}

func TestValidateSessionToken(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)

	identity := &core.UserIdentity{
		Email:       "user@example.com",
		DisplayName: "Test User",
		AvatarURL:   "https://example.com/avatar.png",
	}

	token, err := p.IssueSessionToken(identity)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	got, err := p.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if got.Email != identity.Email {
		t.Errorf("email = %q, want %q", got.Email, identity.Email)
	}
	if got.DisplayName != identity.DisplayName {
		t.Errorf("display_name = %q, want %q", got.DisplayName, identity.DisplayName)
	}
	if got.AvatarURL != identity.AvatarURL {
		t.Errorf("avatar_url = %q, want %q", got.AvatarURL, identity.AvatarURL)
	}
}

func TestValidateExpiredSessionToken(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.ttl = -time.Hour
	})

	identity := &core.UserIdentity{
		Email:       "user@example.com",
		DisplayName: "Test User",
	}

	token, err := p.IssueSessionToken(identity)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	_, err = p.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error should mention expiry: %v", err)
	}
}

func TestName(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)
	if p.Name() != "oidc" {
		t.Errorf("Name() = %q, want %q", p.Name(), "oidc")
	}
}

func TestDefaultScopes(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)
	scopes := p.oauth2Cfg.Scopes

	expected := []string{"openid", "email", "profile"}
	if len(scopes) != len(expected) {
		t.Fatalf("scopes = %v, want %v", scopes, expected)
	}
	for i, s := range expected {
		if scopes[i] != s {
			t.Errorf("scope[%d] = %q, want %q", i, scopes[i], s)
		}
	}
}

func TestDomainRestrictionOnValidateToken(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newDomainRestrictedProvider(t, mockServer.URL, []string{"allowed.com"})

	_, err := p.ValidateToken(context.Background(), "valid-token")
	if err == nil {
		t.Fatal("expected error for disallowed domain on ValidateToken")
	}
}

func TestSessionTokenInvalidSignature(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p1 := newTestProvider(t, mockServer.URL)

	identity := &core.UserIdentity{
		Email:       "user@example.com",
		DisplayName: "Test User",
	}
	token, err := p1.IssueSessionToken(identity)
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	p2 := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.secret = []byte("different-secret-key-32-bytes!!")
	})

	_, err = p2.ValidateToken(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong signature")
	}
}

func TestHandleCallbackWithState(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.pkce = true
	})

	encoded, err := p.encodePKCEState("csrf-token", "test-verifier")
	if err != nil {
		t.Fatalf("encodePKCEState: %v", err)
	}

	identity, origState, err := p.HandleCallbackWithState(context.Background(), "valid-code", encoded)
	if err != nil {
		t.Fatalf("HandleCallbackWithState: %v", err)
	}
	if origState != "csrf-token" {
		t.Errorf("original state = %q, want %q", origState, "csrf-token")
	}
	if identity.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", identity.Email, "user@example.com")
	}
}

func TestHandleCallbackWithStateNonPKCE(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)

	identity, origState, err := p.HandleCallbackWithState(context.Background(), "valid-code", "plain-state")
	if err != nil {
		t.Fatalf("HandleCallbackWithState: %v", err)
	}
	if origState != "plain-state" {
		t.Errorf("original state = %q, want %q", origState, "plain-state")
	}
	if identity.Email != "user@example.com" {
		t.Errorf("email = %q, want %q", identity.Email, "user@example.com")
	}
}

func TestDisplayNameDefault(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)
	if p.DisplayName() != "SSO" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "SSO")
	}
}

func TestDisplayNameCustom(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.displayName = "Okta"
	})
	if p.DisplayName() != "Okta" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Okta")
	}
}

func TestNewConfigValidation(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing issuer URL", Config{ClientID: "id", RedirectURL: "u", SessionSecret: "k"}},
		{"missing client ID", Config{IssuerURL: mockServer.URL, RedirectURL: "u", SessionSecret: "k"}},
		{"missing redirect URL", Config{IssuerURL: mockServer.URL, ClientID: "id", SessionSecret: "k"}},
		{"missing session secret", Config{IssuerURL: mockServer.URL, ClientID: "id", RedirectURL: "u"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(tc.cfg)
			if err == nil {
				t.Error("expected error for invalid config")
			}
		})
	}
}
