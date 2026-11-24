package server_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/bootstrap"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const startOAuthTestProvider = "dcr-app"

// erroringStartHandler is an OAuth handler that can say why it cannot start,
// as mcp_oauth does when discovery or registration fails.
type erroringStartHandler struct {
	testOAuthHandler
	startErr error
}

func (h *erroringStartHandler) StartOAuthE(string, []string) (string, string, error) {
	return "", "", h.startErr
}

func newStartOAuthTestServer(t *testing.T, handler bootstrap.OAuthHandler) *httptest.Server {
	t.Helper()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &stubIntegrationWithAuthURL{
			StubIntegration: coretesting.StubIntegration{N: startOAuthTestProvider},
			authURL:         "https://idp.example.test/authorize",
		})
		cfg.DefaultConnection = map[string]string{startOAuthTestProvider: testDefaultConnection}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			startOAuthTestProvider: {
				Connections: map[string]*config.ConnectionDef{
					testDefaultConnection: {
						Auth: config.ConnectionAuthDef{
							Type:             providermanifestv1.AuthTypeOAuth2,
							AuthorizationURL: "https://idp.example.test/authorize",
							TokenURL:         "https://idp.example.test/token",
						},
					},
				},
			},
		}
		cfg.ConnectionAuth = testConnectionAuth(startOAuthTestProvider, handler)
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func postStartOAuth(t *testing.T, baseURL string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(baseURL+"/api/v1/auth/start-oauth", "application/json",
		bytes.NewBufferString(`{"integration":"`+startOAuthTestProvider+`"}`))
	if err != nil {
		t.Fatalf("POST start-oauth: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("POST start-oauth: body is not JSON: %s", raw)
	}
	return resp.StatusCode, decoded
}

func TestStartOAuthReportsWhyItCannotStart(t *testing.T) {
	t.Parallel()

	ts := newStartOAuthTestServer(t, &erroringStartHandler{
		startErr: errors.New("DCR endpoint returned 400: client registration is not enabled for this application"),
	})

	status, body := postStartOAuth(t, ts.URL)

	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %v", status, body)
	}
	if body["code"] != "connection_unavailable" {
		t.Fatalf("code = %v, want connection_unavailable", body["code"])
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "client registration is not enabled") {
		t.Fatalf("error = %q, want the registration reason", msg)
	}
	if _, ok := body["url"]; ok {
		t.Fatalf("response carries a url on failure: %v", body)
	}
}

func TestStartOAuthRefusesAnEmptyAuthorizationURL(t *testing.T) {
	t.Parallel()

	ts := newStartOAuthTestServer(t, &testOAuthHandler{
		startOAuthFn: func(string, []string) (string, string) { return "", "" },
	})

	status, body := postStartOAuth(t, ts.URL)

	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 rather than a 200 with a hostless ?state= link: %v", status, body)
	}
	if body["code"] != "connection_unavailable" {
		t.Fatalf("code = %v, want connection_unavailable", body["code"])
	}
}
