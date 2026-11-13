package mcpoauth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpoauth"
)

// When registration is refused, StartOAuthE must return the reason rather
// than an empty URL.
func TestStartOAuthEReportsRegistrationFailure(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		writeJSON(w, 0, map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		writeJSON(w, 0, map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/oauth/authorize",
			"token_endpoint":                        base + "/oauth/token",
			"registration_endpoint":                 base + "/oauth/register",
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none"},
		})
	})
	mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":             "invalid_client_metadata",
			"error_description": "client registration is not enabled for this application",
		})
	})
	srv := httptest.NewServer(mux)
	testutil.CloseOnCleanup(t, srv)

	handler := mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:      srv.URL + "/mcp",
		Credentials: coretesting.NewStubExternalCredentialProvider(),
		RedirectURL: testRedirectURL,
	})

	authURL, verifier, err := handler.StartOAuthE("state", nil)
	if err == nil {
		t.Fatalf("StartOAuthE = (%q, %q, nil), want the registration error", authURL, verifier)
	}
	if authURL != "" {
		t.Fatalf("authURL = %q, want empty on failure", authURL)
	}
	if !strings.Contains(err.Error(), "client registration is not enabled") {
		t.Fatalf("error = %v, want the server's reason", err)
	}

	// StartOAuth keeps its old contract: an empty URL, no panic.
	if authURL, _ := handler.StartOAuth("state", nil); authURL != "" {
		t.Fatalf("StartOAuth = %q, want empty on failure", authURL)
	}
}
