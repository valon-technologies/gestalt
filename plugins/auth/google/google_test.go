package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/toolshed/core"
	coretesting "github.com/valon-technologies/toolshed/core/testing"
	"golang.org/x/oauth2"
)

func newMockGoogleServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

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
			_ = json.NewEncoder(w).Encode(map[string]string{
				"email":   "user@example.com",
				"name":    "Test User",
				"picture": "https://example.com/avatar.png",
			})
		default:
			http.Error(w, "invalid token", http.StatusUnauthorized)
		}
	})

	return httptest.NewServer(mux)
}

func newTestProvider(t *testing.T, mockURL string, opts ...func(*Provider)) *Provider {
	t.Helper()
	p, err := New(Config{
		ClientID:      "test-client-id",
		ClientSecret:  "test-client-secret",
		RedirectURL:   "http://localhost/callback",
		SessionSecret: []byte("test-secret-key-32-bytes-long!!!"),
		SessionTTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	p.oauth2Config.Endpoint = oauth2.Endpoint{
		AuthURL:  mockURL + "/auth",
		TokenURL: mockURL + "/token",
	}
	p.userinfoURL = mockURL + "/userinfo"
	p.httpClient = &http.Client{}

	for _, fn := range opts {
		fn(p)
	}
	return p
}

func newDomainRestrictedProvider(t *testing.T, mockURL string, domains []string) *Provider {
	t.Helper()
	p, err := New(Config{
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		RedirectURL:    "http://localhost/callback",
		AllowedDomains: domains,
		SessionSecret:  []byte("test-secret-key-32-bytes-long!!!"),
		SessionTTL:     time.Hour,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.oauth2Config.Endpoint = oauth2.Endpoint{
		AuthURL:  mockURL + "/auth",
		TokenURL: mockURL + "/token",
	}
	p.userinfoURL = mockURL + "/userinfo"
	p.httpClient = &http.Client{}
	return p
}

func TestGoogleAuthConformance(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
	defer mockServer.Close()

	coretesting.RunAuthProviderTests(t, func(t *testing.T, mockURL string) core.AuthProvider {
		return newTestProvider(t, mockURL)
	}, mockServer)
}

func TestLoginURLContainsScopes(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)
	url := p.LoginURL("my-state")
	for _, scope := range []string{"openid", "email", "profile"} {
		if !strings.Contains(url, scope) {
			t.Errorf("LoginURL missing scope %q: %s", scope, url)
		}
	}
}

func TestSessionTokenRoundTrip(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
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
}

func TestSessionTokenExpired(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL, func(p *Provider) {
		p.ttl = -time.Hour // already expired
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

func TestSessionTokenInvalidSignature(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
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

func TestDomainRestriction(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
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

func TestDomainRestrictionOnValidateToken(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
	defer mockServer.Close()

	p := newDomainRestrictedProvider(t, mockServer.URL, []string{"allowed.com"})

	_, err := p.ValidateToken(context.Background(), "valid-token")
	if err == nil {
		t.Fatal("expected error for disallowed domain on ValidateToken")
	}
}

func TestDisplayName(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
	defer mockServer.Close()

	p := newTestProvider(t, mockServer.URL)
	if p.DisplayName() != "Google" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Google")
	}
}

func TestNewConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing client ID", Config{ClientSecret: "s", RedirectURL: "u", SessionSecret: []byte("k")}},
		{"missing client secret", Config{ClientID: "i", RedirectURL: "u", SessionSecret: []byte("k")}},
		{"missing redirect URL", Config{ClientID: "i", ClientSecret: "s", SessionSecret: []byte("k")}},
		{"missing session secret", Config{ClientID: "i", ClientSecret: "s", RedirectURL: "u"}},
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
