package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestBrowserSessionCookieAuthorizesHTTPOperations(t *testing.T) {
	t.Parallel()
	const sessionToken = "browser-login-token"
	tests := []struct {
		name      string
		path      string
		body      string
		newServer func(*testing.T, *coretesting.StubAuthProvider, *coretesting.StubIntegration, func() time.Time) *httptest.Server
		wantBody  string
	}{
		{
			name:     "v1",
			path:     "/api/v1/example/browser",
			body:     `{}`,
			wantBody: "browser-ok",
			newServer: func(t *testing.T, auth *coretesting.StubAuthProvider, provider *coretesting.StubIntegration, now func() time.Time) *httptest.Server {
				return newTestServer(t, func(cfg *server.Config) {
					cfg.Auth = auth
					cfg.Now = now
					cfg.Providers = testutil.NewProviderRegistry(t, provider)
				})
			},
		},
		{
			name: "v2 public REST",
			path: "/api/v2/app/example/operations/browser",
			body: `{"params":{}}`,
			newServer: func(t *testing.T, auth *coretesting.StubAuthProvider, provider *coretesting.StubIntegration, now func() time.Time) *httptest.Server {
				return startPublicRESTServer(t, server.RouteProfilePublic, func(cfg *server.Config) {
					cfg.Auth = auth
					cfg.Now = now
					cfg.PublicGatewayTransport.SetIdentityProvider(auth)
					cfg.Providers = testutil.NewProviderRegistry(t, provider)
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var now atomic.Int64
			now.Store(time.Now().UTC().Unix())
			var lastIntrospected atomic.Value
			lastIntrospected.Store("")

			auth := &coretesting.StubAuthProvider{
				N: "test",
				TokenFn: func(_ context.Context, req *core.TokenRequest) (*core.TokenResponse, error) {
					if req == nil || req.Code != "good-code" {
						return nil, fmt.Errorf("unexpected authorization code")
					}
					return &core.TokenResponse{AccessToken: sessionToken, TokenType: "Bearer", ExpiresIn: 3600, GrantID: "browser-grant"}, nil
				},
				IntrospectFn: func(_ context.Context, req *core.IntrospectRequest) (*core.IntrospectResponse, error) {
					if req == nil {
						return &core.IntrospectResponse{Active: false}, nil
					}
					lastIntrospected.Store(req.Token)
					if req.Token != sessionToken {
						return &core.IntrospectResponse{Active: false}, nil
					}
					return &core.IntrospectResponse{Active: true, Subject: "user:browser-session-user"}, nil
				},
			}
			browser := catalog.APIExposureBrowserSession
			provider := &coretesting.StubIntegration{
				N:        "example",
				ConnMode: core.ConnectionModeNone,
				CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
					{ID: "browser", Method: http.MethodPost, API: &browser},
				}},
				ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
					return &core.OperationResult{Status: http.StatusOK, Body: []byte("browser-ok")}, nil
				},
			}
			ts := tt.newServer(t, auth, provider, func() time.Time { return time.Unix(now.Load(), 0).UTC() })

			// Only the browser login callback can mint the sealed session cookie.
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatalf("create cookie jar: %v", err)
			}
			client := &http.Client{Jar: jar}
			loginStart, err := client.Post(ts.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"state":"test-state"}`))
			if err != nil {
				t.Fatalf("start login: %v", err)
			}
			var loginStateValue string
			for _, cookie := range loginStart.Cookies() {
				if cookie.Name == "login_state" {
					loginStateValue = cookie.Value
					break
				}
			}
			_ = loginStart.Body.Close()
			if loginStateValue == "" {
				t.Fatal("login start did not issue login state cookie")
			}
			callback, err := client.Get(ts.URL + "/api/v1/auth/login/callback?code=good-code&state=test-state")
			if err != nil {
				t.Fatalf("login callback: %v", err)
			}
			defer func() { _ = callback.Body.Close() }()
			if callback.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(callback.Body)
				t.Fatalf("login callback status = %d, want %d: %s", callback.StatusCode, http.StatusOK, body)
			}
			var sessionCookie *http.Cookie
			for _, cookie := range callback.Cookies() {
				if cookie.Name == "session_token" {
					sessionCookie = cookie
					break
				}
			}
			if sessionCookie == nil || !strings.HasPrefix(sessionCookie.Value, "gst_browser_session.") {
				t.Fatalf("login callback session cookie = %#v, want sealed browser session", sessionCookie)
			}

			invoke := func(cookieValue, bearer string) (int, []byte) {
				t.Helper()
				req, err := http.NewRequest(http.MethodPost, ts.URL+tt.path, strings.NewReader(tt.body))
				if err != nil {
					t.Fatal(err)
				}
				if cookieValue != "" {
					req.AddCookie(&http.Cookie{Name: "session_token", Value: cookieValue})
				}
				if bearer != "" {
					req.Header.Set("Authorization", "Bearer "+bearer)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = resp.Body.Close() }()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				return resp.StatusCode, body
			}

			if status, body := invoke(sessionCookie.Value, ""); status != http.StatusOK || (tt.wantBody != "" && string(body) != tt.wantBody) {
				t.Fatalf("sealed browser session invoke = %d/%s, want 200/%s", status, body, tt.wantBody)
			}
			if got := lastIntrospected.Load().(string); got != sessionToken {
				t.Fatalf("identity provider introspected %q, want callback token %q", got, sessionToken)
			}
			if status, _ := invoke("", sessionToken); status != http.StatusForbidden {
				t.Fatalf("bearer token status = %d, want %d", status, http.StatusForbidden)
			}
			if status, _ := invoke(sessionToken, ""); status != http.StatusForbidden {
				t.Fatalf("raw session cookie status = %d, want %d", status, http.StatusForbidden)
			}

			const prefix = "gst_browser_session."
			tampered := sessionCookie.Value
			if len(tampered) <= len(prefix) {
				t.Fatal("sealed session cookie is unexpectedly short")
			}
			flip := byte('A')
			if tampered[len(prefix)] == flip {
				flip = 'B'
			}
			tampered = tampered[:len(prefix)] + string(flip) + tampered[len(prefix)+1:]
			if status, _ := invoke(tampered, ""); status != http.StatusUnauthorized {
				t.Fatalf("tampered browser session status = %d, want %d", status, http.StatusUnauthorized)
			}
			if status, _ := invoke(prefix+loginStateValue, ""); status != http.StatusUnauthorized {
				t.Fatalf("wrong-purpose browser session status = %d, want %d", status, http.StatusUnauthorized)
			}

			now.Add(int64((2 * time.Hour).Seconds()))
			if status, _ := invoke(sessionCookie.Value, ""); status != http.StatusUnauthorized {
				t.Fatalf("expired browser session status = %d, want %d", status, http.StatusUnauthorized)
			}
		})
	}
}
