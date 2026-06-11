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
	"time"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpoauth"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	if status != 0 {
		w.WriteHeader(status)
	}
	_ = json.NewEncoder(w).Encode(v)
}

// fakeAuthServer is an MCP resource plus OAuth authorization server with DCR.
// direct=false advertises the AS via RFC 9728 WWW-Authenticate indirection;
// direct=true embeds the endpoints in the resource metadata (ClickHouse style).
type fakeAuthServer struct {
	srv *httptest.Server

	mu             sync.Mutex
	dcrCount       int
	dcrGrantTypes  []any
	tokenClientIDs []string
	tokenVerifiers []string
	// newClientID names the nth registered client; default is client-00n.
	newClientID func(n int) string
}

func newFakeAuthServer(t *testing.T, direct bool) *fakeAuthServer {
	t.Helper()
	as := &fakeAuthServer{newClientID: func(n int) string { return fmt.Sprintf("client-%03d", n) }}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		if !direct {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer resource_metadata="http://%s/.well-known/oauth-protected-resource/mcp"`, r.Host))
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		baseURL := "http://" + r.Host
		if direct {
			writeJSON(w, 0, map[string]any{
				"authorization_endpoint":                baseURL + "/oauth/authorize",
				"token_endpoint":                        baseURL + "/oauth/token",
				"registration_endpoint":                 baseURL + "/oauth/register",
				"scopes_supported":                      []string{"query"},
				"code_challenge_methods_supported":      []string{"S256"},
				"token_endpoint_auth_methods_supported": []string{"none"},
			})
			return
		}
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
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		as.mu.Lock()
		as.dcrCount++
		n := as.dcrCount
		as.dcrGrantTypes, _ = body["grant_types"].([]any)
		as.mu.Unlock()
		// Outside the lock: newClientID may block on a test barrier.
		writeJSON(w, http.StatusCreated, map[string]any{"client_id": as.newClientID(n)})
	})
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") == "" {
			http.Error(w, "bad token request", http.StatusBadRequest)
			return
		}
		as.mu.Lock()
		as.tokenClientIDs = append(as.tokenClientIDs, r.Form.Get("client_id"))
		as.tokenVerifiers = append(as.tokenVerifiers, r.Form.Get("code_verifier"))
		as.mu.Unlock()
		writeJSON(w, 0, map[string]any{
			"access_token":  "access-tok-123",
			"refresh_token": "refresh-tok-456",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})

	as.srv = httptest.NewServer(mux)
	testutil.CloseOnCleanup(t, as.srv)
	return as
}

func newTestStore(t *testing.T) *mcpoauth.Store {
	t.Helper()
	store, err := mcpoauth.NewStore(context.Background(), &coretesting.StubIndexedDB{}, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func newHandler(as *fakeAuthServer, store *mcpoauth.Store) *mcpoauth.Handler {
	return mcpoauth.NewHandler(mcpoauth.HandlerConfig{
		MCPURL:      as.srv.URL + "/mcp",
		Store:       store,
		RedirectURL: "http://localhost:9999/callback",
	})
}

func clientIDOf(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing auth URL %q: %v", authURL, err)
	}
	return parsed.Query().Get("client_id")
}

func TestMCPOAuthFlow(t *testing.T) {
	t.Parallel()

	// The production topology: the OAuth start and the callback are served by
	// different instances that share only the registration store. The exchange
	// must present the same client_id the authorize URL carried.
	t.Run("RFC9728CrossInstance", func(t *testing.T) {
		t.Parallel()

		as := newFakeAuthServer(t, false)
		store := newTestStore(t)
		instanceA := newHandler(as, store)
		instanceB := newHandler(as, store)

		authURL, verifier := instanceA.StartOAuth("test-state", nil)
		if authURL == "" || verifier == "" {
			t.Fatalf("StartOAuth = (%q, %q), want non-empty auth URL and PKCE verifier", authURL, verifier)
		}
		parsed, err := url.Parse(authURL)
		if err != nil {
			t.Fatalf("parsing auth URL: %v", err)
		}
		if !strings.HasPrefix(parsed.Path, "/oauth/authorize") {
			t.Errorf("auth URL path = %q, want /oauth/authorize prefix", parsed.Path)
		}
		if got := parsed.Query().Get("client_id"); got != "client-001" {
			t.Errorf("client_id = %q, want client-001", got)
		}
		if got := parsed.Query().Get("code_challenge_method"); got != "S256" {
			t.Errorf("code_challenge_method = %q, want S256", got)
		}
		if len(as.dcrGrantTypes) != 2 || as.dcrGrantTypes[0] != "authorization_code" || as.dcrGrantTypes[1] != "refresh_token" {
			t.Errorf("DCR grant_types = %v, want [authorization_code, refresh_token]", as.dcrGrantTypes)
		}

		reg, err := store.Get(context.Background(), as.srv.URL, "http://localhost:9999/callback")
		if err != nil || reg == nil {
			t.Fatalf("store.Get = (%v, %v), want stored registration", reg, err)
		}
		if reg.ClientID != "client-001" {
			t.Errorf("stored client_id = %q, want client-001", reg.ClientID)
		}

		tokenResp, err := instanceB.ExchangeCodeWithVerifier(context.Background(), "auth-code-xyz", verifier)
		if err != nil {
			t.Fatalf("ExchangeCodeWithVerifier: %v", err)
		}
		if tokenResp.AccessToken != "access-tok-123" || tokenResp.RefreshToken != "refresh-tok-456" {
			t.Errorf("token response = (%q, %q), want (access-tok-123, refresh-tok-456)", tokenResp.AccessToken, tokenResp.RefreshToken)
		}
		if as.dcrCount != 1 {
			t.Errorf("DCR calls = %d, want 1 (instance B must reuse instance A's client)", as.dcrCount)
		}
		if len(as.tokenClientIDs) != 1 || as.tokenClientIDs[0] != "client-001" {
			t.Errorf("token exchange client_ids = %v, want [client-001]", as.tokenClientIDs)
		}
		if len(as.tokenVerifiers) != 1 || as.tokenVerifiers[0] != verifier {
			t.Errorf("token exchange verifier not forwarded")
		}
	})

	t.Run("DirectEndpoints", func(t *testing.T) {
		t.Parallel()

		as := newFakeAuthServer(t, true)
		handler := newHandler(as, newTestStore(t))

		authURL, verifier := handler.StartOAuth("test-state", nil)
		if got := clientIDOf(t, authURL); got != "client-001" {
			t.Errorf("client_id = %q, want client-001", got)
		}
		tokenResp, err := handler.ExchangeCodeWithVerifier(context.Background(), "test-code", verifier)
		if err != nil {
			t.Fatalf("ExchangeCodeWithVerifier: %v", err)
		}
		if tokenResp.AccessToken != "access-tok-123" {
			t.Errorf("access_token = %q, want access-tok-123", tokenResp.AccessToken)
		}
	})

	t.Run("ConcurrentDCRFirstWriterWins", func(t *testing.T) {
		t.Parallel()

		as := newFakeAuthServer(t, false)
		barrier := make(chan struct{})
		release := sync.OnceFunc(func() { close(barrier) })
		as.newClientID = func(n int) string {
			// Hold the first registration until the second arrives so both
			// instances register before either stores its client.
			if n == 1 {
				<-barrier
			} else {
				release()
			}
			return fmt.Sprintf("client-%03d", n)
		}
		store := newTestStore(t)
		handlers := []*mcpoauth.Handler{newHandler(as, store), newHandler(as, store)}

		urls := make([]string, len(handlers))
		var wg sync.WaitGroup
		for i, h := range handlers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				urls[i], _ = h.StartOAuth(fmt.Sprintf("s%d", i), nil)
			}()
		}
		wg.Wait()
		release()

		clientA, clientB := clientIDOf(t, urls[0]), clientIDOf(t, urls[1])
		if clientA == "" || clientA != clientB {
			t.Fatalf("authorize client_ids = (%q, %q), want both equal to the race winner", clientA, clientB)
		}
		reg, err := store.Get(context.Background(), as.srv.URL, "http://localhost:9999/callback")
		if err != nil || reg == nil {
			t.Fatalf("store.Get = (%v, %v), want stored registration", reg, err)
		}
		if reg.ClientID != clientA {
			t.Errorf("stored client_id = %q, want %q", reg.ClientID, clientA)
		}
	})

	t.Run("ExpiredRegistrationReRegisters", func(t *testing.T) {
		t.Parallel()

		as := newFakeAuthServer(t, false)
		store := newTestStore(t)
		expired := time.Now().Add(-time.Hour)
		err := store.Put(context.Background(), as.srv.URL, "http://localhost:9999/callback",
			&mcpoauth.Registration{ClientID: "stale-client", ExpiresAt: &expired})
		if err != nil {
			t.Fatalf("seeding expired registration: %v", err)
		}

		authURL, _ := newHandler(as, store).StartOAuth("s1", nil)
		if got := clientIDOf(t, authURL); got != "client-001" {
			t.Errorf("client_id = %q, want re-registered client-001", got)
		}
		reg, err := store.Get(context.Background(), as.srv.URL, "http://localhost:9999/callback")
		if err != nil || reg == nil || reg.ClientID != "client-001" {
			t.Errorf("store.Get = (%v, %v), want replaced registration client-001", reg, err)
		}
	})

	t.Run("IsolatedFromDefaultTransportCloseIdleConnections", func(t *testing.T) {
		t.Parallel()

		as := newFakeAuthServer(t, false)
		handler := newHandler(as, newTestStore(t))

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
			if authURL == "" || verifier == "" {
				t.Fatalf("StartOAuth #%d = (%q, %q), want non-empty", i+1, authURL, verifier)
			}
			if got := clientIDOf(t, authURL); got != "client-001" {
				t.Fatalf("client_id #%d = %q, want client-001", i+1, got)
			}
		}
	})
}
