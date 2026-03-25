package oidc

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
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

	secret := []byte("test-secret-key-32-bytes-long!!!")
	encKey := sha256.Sum256(secret)
	enc, err := crypto.NewAESGCM(encKey[:])
	if err != nil {
		t.Fatalf("init encryptor: %v", err)
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
		secret:      secret,
		encryptor:   enc,
		ttl:         time.Hour,
		displayName: "SSO",
	}

	for _, fn := range opts {
		fn(p)
	}
	return p
}

func TestOIDCAuthConformance(t *testing.T) {
	t.Parallel()
	mockServer := newMockOIDCServer(t)
	defer mockServer.Close()

	coretesting.RunAuthProviderTests(t, func(t *testing.T, mockURL string) core.AuthProvider {
		return newTestProvider(t, mockURL)
	}, mockServer)
}
