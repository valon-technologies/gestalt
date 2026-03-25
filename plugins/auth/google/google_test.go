package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
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

func TestGoogleAuthConformance(t *testing.T) {
	t.Parallel()
	mockServer := newMockGoogleServer(t)
	defer mockServer.Close()

	coretesting.RunAuthProviderTests(t, func(t *testing.T, mockURL string) core.AuthProvider {
		return newTestProvider(t, mockURL)
	}, mockServer)
}
