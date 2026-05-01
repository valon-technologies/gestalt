package mcpoauth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/plugins/mcpoauth"
)

type memStore struct {
	mu   sync.Mutex
	regs map[string]*mcpoauth.Registration
}

func (s *memStore) GetRegistration(_ context.Context, authServerURL, redirectURI string) (*mcpoauth.Registration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.regs[authServerURL+"|"+redirectURI], nil
}

func (s *memStore) StoreRegistration(_ context.Context, reg *mcpoauth.Registration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.regs[reg.AuthServerURL+"|"+reg.RedirectURI] = reg
	return nil
}

func (s *memStore) DeleteRegistration(_ context.Context, authServerURL, redirectURI string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.regs, authServerURL+"|"+redirectURI)
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	if status != 0 {
		w.WriteHeader(status)
	}
	_ = json.NewEncoder(w).Encode(v)
}

func TestMCPOAuthFlow(t *testing.T) {
	t.Parallel()

	t.Run("RFC9728", func(t *testing.T) {
		t.Parallel()

		var dcrBody map[string]any

		mux := http.NewServeMux()

		mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, baseURL))
			w.WriteHeader(http.StatusUnauthorized)
		})

		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"resource":              baseURL + "/mcp",
				"authorization_servers": []string{baseURL},
				"scopes_supported":      []string{"read", "write"},
			})
		})

		mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"issuer":                                baseURL,
				"authorization_endpoint":                baseURL + "/oauth/authorize",
				"token_endpoint":                        baseURL + "/oauth/token",
				"registration_endpoint":                 baseURL + "/oauth/register",
				"scopes_supported":                      []string{"read", "write"},
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"},
			})
		})

		mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&dcrBody)
			writeJSON(w, http.StatusCreated, map[string]any{
				"client_id": "dcr-client-001",
			})
		})

		mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "authorization_code" {
				http.Error(w, "bad grant_type", http.StatusBadRequest)
				return
			}
			if r.Form.Get("code") == "" {
				http.Error(w, "missing code", http.StatusBadRequest)
				return
			}
			writeJSON(w, 0, map[string]any{
				"access_token":  "access-tok-123",
				"refresh_token": "refresh-tok-456",
				"token_type":    "Bearer",
				"expires_in":    3600,
			})
		})

		srv := httptest.NewServer(mux)
		testutil.CloseOnCleanup(t, srv)

		store := &memStore{regs: make(map[string]*mcpoauth.Registration)}
		handler := mcpoauth.NewHandler(mcpoauth.HandlerConfig{
			MCPURL:      srv.URL + "/mcp",
			Store:       store,
			RedirectURL: "http://localhost:9999/callback",
		})

		authURL, verifier := handler.StartOAuth("test-state", nil)

		if authURL == "" {
			t.Fatal("expected non-empty auth URL")
		}
		if verifier == "" {
			t.Fatal("expected non-empty PKCE verifier")
		}

		parsed, err := url.Parse(authURL)
		if err != nil {
			t.Fatalf("parsing auth URL: %v", err)
		}
		if !strings.HasPrefix(parsed.Path, "/oauth/authorize") {
			t.Errorf("auth URL path = %q, want /oauth/authorize prefix", parsed.Path)
		}
		if parsed.Query().Get("client_id") != "dcr-client-001" {
			t.Errorf("client_id = %q, want dcr-client-001", parsed.Query().Get("client_id"))
		}
		if parsed.Query().Get("code_challenge_method") != "S256" {
			t.Errorf("code_challenge_method = %q, want S256", parsed.Query().Get("code_challenge_method"))
		}

		grantTypes, _ := dcrBody["grant_types"].([]any)
		if len(grantTypes) != 2 {
			t.Fatalf("DCR grant_types length = %d, want 2", len(grantTypes))
		}
		if grantTypes[0] != "authorization_code" || grantTypes[1] != "refresh_token" {
			t.Errorf("DCR grant_types = %v, want [authorization_code, refresh_token]", grantTypes)
		}

		reg, _ := store.GetRegistration(context.Background(), srv.URL, "http://localhost:9999/callback")
		if reg == nil {
			t.Fatal("expected stored registration")
			return
		}
		if reg.ClientID != "dcr-client-001" {
			t.Errorf("stored client_id = %q, want dcr-client-001", reg.ClientID)
		}

		tokenResp, err := handler.ExchangeCodeWithVerifier(context.Background(), "auth-code-xyz", verifier)
		if err != nil {
			t.Fatalf("ExchangeCodeWithVerifier: %v", err)
		}
		if tokenResp.AccessToken != "access-tok-123" {
			t.Errorf("access_token = %q, want access-tok-123", tokenResp.AccessToken)
		}
		if tokenResp.RefreshToken != "refresh-tok-456" {
			t.Errorf("refresh_token = %q, want refresh-tok-456", tokenResp.RefreshToken)
		}
	})

	t.Run("DirectEndpoints", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()

		mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})

		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"authorization_endpoint":                baseURL + "/oauth/authorize",
				"token_endpoint":                        baseURL + "/oauth/token",
				"registration_endpoint":                 baseURL + "/oauth/register",
				"scopes_supported":                      []string{"query"},
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		})

		mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]any{
				"client_id": "direct-client-001",
			})
		})

		mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Form.Get("client_id") == "" {
				http.Error(w, "missing client_id", http.StatusBadRequest)
				return
			}
			writeJSON(w, 0, map[string]any{
				"access_token": "direct-access-tok",
				"token_type":   "Bearer",
				"expires_in":   7200,
			})
		})

		srv := httptest.NewServer(mux)
		testutil.CloseOnCleanup(t, srv)

		store := &memStore{regs: make(map[string]*mcpoauth.Registration)}
		handler := mcpoauth.NewHandler(mcpoauth.HandlerConfig{
			MCPURL:      srv.URL + "/mcp",
			Store:       store,
			RedirectURL: "http://localhost:9999/callback",
		})

		authURL, verifier := handler.StartOAuth("test-state", nil)
		if authURL == "" {
			t.Fatal("expected non-empty auth URL")
		}

		parsed, _ := url.Parse(authURL)
		if parsed.Query().Get("client_id") != "direct-client-001" {
			t.Errorf("client_id = %q, want direct-client-001", parsed.Query().Get("client_id"))
		}

		tokenResp, err := handler.ExchangeCodeWithVerifier(context.Background(), "test-code", verifier)
		if err != nil {
			t.Fatalf("ExchangeCodeWithVerifier: %v", err)
		}
		if tokenResp.AccessToken != "direct-access-tok" {
			t.Errorf("access_token = %q, want direct-access-tok", tokenResp.AccessToken)
		}
	})

	t.Run("EphemeralDCRWithoutStore", func(t *testing.T) {
		t.Parallel()

		var dcrCount int
		mux := http.NewServeMux()

		mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, baseURL))
			w.WriteHeader(http.StatusUnauthorized)
		})

		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"resource":              baseURL + "/mcp",
				"authorization_servers": []string{baseURL},
				"scopes_supported":      []string{"read"},
			})
		})

		mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"issuer":                                baseURL,
				"authorization_endpoint":                baseURL + "/oauth/authorize",
				"token_endpoint":                        baseURL + "/oauth/token",
				"registration_endpoint":                 baseURL + "/oauth/register",
				"scopes_supported":                      []string{"read"},
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		})

		mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, r *http.Request) {
			dcrCount++
			writeJSON(w, http.StatusCreated, map[string]any{
				"client_id": "ephemeral-client-001",
			})
		})

		srv := httptest.NewServer(mux)
		testutil.CloseOnCleanup(t, srv)

		handler := mcpoauth.NewHandler(mcpoauth.HandlerConfig{
			MCPURL:      srv.URL + "/mcp",
			RedirectURL: "http://localhost:9999/callback",
		})

		authURL, verifier := handler.StartOAuth("test-state", nil)
		if authURL == "" {
			t.Fatal("expected non-empty auth URL")
		}
		if verifier == "" {
			t.Fatal("expected non-empty verifier")
		}
		if dcrCount != 1 {
			t.Fatalf("DCR calls = %d, want 1", dcrCount)
		}

		parsed, err := url.Parse(authURL)
		if err != nil {
			t.Fatalf("parsing auth URL: %v", err)
		}
		if parsed.Query().Get("client_id") != "ephemeral-client-001" {
			t.Errorf("client_id = %q, want ephemeral-client-001", parsed.Query().Get("client_id"))
		}

		authURL, verifier = handler.StartOAuth("test-state-2", nil)
		if authURL == "" || verifier == "" {
			t.Fatal("expected cached upstream for repeated StartOAuth")
		}
		if dcrCount != 1 {
			t.Fatalf("DCR calls after cache reuse = %d, want 1", dcrCount)
		}
	})

	t.Run("ClearRegistration", func(t *testing.T) {
		t.Parallel()

		var dcrCount int
		var mu sync.Mutex

		mux := http.NewServeMux()
		mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, baseURL))
			w.WriteHeader(http.StatusUnauthorized)
		})
		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"resource":              baseURL + "/mcp",
				"authorization_servers": []string{baseURL},
			})
		})
		mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"issuer":                                baseURL,
				"authorization_endpoint":                baseURL + "/oauth/authorize",
				"token_endpoint":                        baseURL + "/oauth/token",
				"registration_endpoint":                 baseURL + "/oauth/register",
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		})
		mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			dcrCount++
			id := fmt.Sprintf("client-%03d", dcrCount)
			mu.Unlock()
			writeJSON(w, http.StatusCreated, map[string]any{"client_id": id})
		})

		srv := httptest.NewServer(mux)
		testutil.CloseOnCleanup(t, srv)

		store := &memStore{regs: make(map[string]*mcpoauth.Registration)}
		handler := mcpoauth.NewHandler(mcpoauth.HandlerConfig{
			MCPURL:      srv.URL + "/mcp",
			Store:       store,
			RedirectURL: "http://localhost:9999/callback",
		})

		authURL1, _ := handler.StartOAuth("s1", nil)
		parsed1, _ := url.Parse(authURL1)
		if parsed1.Query().Get("client_id") != "client-001" {
			t.Fatalf("first client_id = %q, want client-001", parsed1.Query().Get("client_id"))
		}

		authURL2, _ := handler.StartOAuth("s2", nil)
		parsed2, _ := url.Parse(authURL2)
		if parsed2.Query().Get("client_id") != "client-001" {
			t.Fatalf("cached client_id = %q, want client-001", parsed2.Query().Get("client_id"))
		}

		handler.ClearRegistration()

		authURL3, _ := handler.StartOAuth("s3", nil)
		parsed3, _ := url.Parse(authURL3)
		if parsed3.Query().Get("client_id") != "client-002" {
			t.Fatalf("re-registered client_id = %q, want client-002", parsed3.Query().Get("client_id"))
		}
	})

	t.Run("IsolatedFromDefaultTransportCloseIdleConnections", func(t *testing.T) {
		t.Parallel()

		mux := http.NewServeMux()
		mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp"`, baseURL))
			w.WriteHeader(http.StatusUnauthorized)
		})
		mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"resource":              baseURL + "/mcp",
				"authorization_servers": []string{baseURL},
			})
		})
		mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
			baseURL := "http://" + r.Host
			writeJSON(w, 0, map[string]any{
				"issuer":                                baseURL,
				"authorization_endpoint":                baseURL + "/oauth/authorize",
				"token_endpoint":                        baseURL + "/oauth/token",
				"registration_endpoint":                 baseURL + "/oauth/register",
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
		})
		mux.HandleFunc("POST /oauth/register", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusCreated, map[string]any{"client_id": "client-001"})
		})

		srv := httptest.NewServer(mux)
		testutil.CloseOnCleanup(t, srv)

		handler := mcpoauth.NewHandler(mcpoauth.HandlerConfig{
			MCPURL:      srv.URL + "/mcp",
			RedirectURL: "http://localhost:9999/callback",
		})

		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			t.Fatalf("http.DefaultTransport = %T, want *http.Transport", http.DefaultTransport)
		}

		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				default:
					defaultTransport.CloseIdleConnections()
				}
			}
		}()
		t.Cleanup(func() {
			close(stop)
			<-done
		})

		for i := range 5 {
			authURL, verifier := handler.StartOAuth(fmt.Sprintf("s%d", i+1), nil)
			if authURL == "" {
				t.Fatalf("StartOAuth #%d returned empty authURL", i+1)
			}
			if verifier == "" {
				t.Fatalf("StartOAuth #%d returned empty verifier", i+1)
			}
			parsed, err := url.Parse(authURL)
			if err != nil {
				t.Fatalf("parse auth URL #%d: %v", i+1, err)
			}
			if parsed.Query().Get("client_id") != "client-001" {
				t.Fatalf("client_id #%d = %q, want client-001", i+1, parsed.Query().Get("client_id"))
			}
			handler.ClearRegistration()
		}
	})
}
