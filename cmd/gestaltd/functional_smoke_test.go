package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/internal/testutil"
)

func TestGestaltdFunctionalSmoke(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := testutil.FreePort(t)
	cfgPath := testutil.WriteConfigFile(t, dir, functionalSmokeConfig(dir, port))
	srv := testutil.StartGestaltd(t, cfgPath, port)

	client := testutil.NewCookieClient(t)
	testutil.DevLogin(t, client, srv.BaseURL, "functional@gestalt.dev")

	status, body := testutil.DoJSON(t, client, http.MethodGet, srv.BaseURL+"/health", nil)
	if status != http.StatusOK {
		t.Fatalf("health status=%d body=%s", status, string(body))
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, srv.BaseURL+"/ready", nil)
	if status != http.StatusOK {
		t.Fatalf("ready status=%d body=%s", status, string(body))
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, srv.BaseURL+"/api/v1/integrations", nil)
	if status != http.StatusOK {
		t.Fatalf("list integrations status=%d body=%s", status, string(body))
	}
	integrations := testutil.DecodeJSON[[]struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}](t, body)

	foundEcho := false
	for _, integration := range integrations {
		if integration.Name == "echo" {
			foundEcho = true
			if integration.DisplayName != "Echo" {
				t.Fatalf("echo display name=%q", integration.DisplayName)
			}
		}
	}
	if !foundEcho {
		t.Fatalf("echo integration missing from %+v", integrations)
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, srv.BaseURL+"/api/v1/integrations/echo/operations", nil)
	if status != http.StatusOK {
		t.Fatalf("list operations status=%d body=%s", status, string(body))
	}
	operations := testutil.DecodeJSON[[]struct {
		Name   string
		Method string
	}](t, body)
	if len(operations) != 1 || operations[0].Name != "echo" || operations[0].Method != http.MethodPost {
		t.Fatalf("unexpected echo operations: %+v", operations)
	}

	status, body = testutil.DoJSON(t, client, http.MethodPost, srv.BaseURL+"/api/v1/echo/echo", map[string]any{
		"message": "hello",
		"count":   2,
	})
	if status != http.StatusOK {
		t.Fatalf("invoke echo status=%d body=%s", status, string(body))
	}
	echoed := testutil.DecodeJSON[map[string]any](t, body)
	if echoed["message"] != "hello" {
		t.Fatalf("echoed message=%v", echoed["message"])
	}
	if echoed["count"] != float64(2) {
		t.Fatalf("echoed count=%v", echoed["count"])
	}

	status, body = testutil.DoJSON(t, client, http.MethodPost, srv.BaseURL+"/api/v1/tokens", map[string]any{
		"name": "functional-token",
	})
	if status != http.StatusCreated {
		t.Fatalf("create token status=%d body=%s", status, string(body))
	}
	created := testutil.DecodeJSON[map[string]any](t, body)
	tokenID, _ := created["id"].(string)
	tokenValue, _ := created["token"].(string)
	if tokenID == "" || tokenValue == "" {
		t.Fatalf("unexpected token response: %+v", created)
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, srv.BaseURL+"/api/v1/tokens", nil)
	if status != http.StatusOK {
		t.Fatalf("list tokens status=%d body=%s", status, string(body))
	}
	tokens := testutil.DecodeJSON[[]map[string]any](t, body)
	if len(tokens) != 1 || tokens[0]["name"] != "functional-token" {
		t.Fatalf("unexpected tokens after create: %+v", tokens)
	}

	status, body = testutil.DoJSON(t, client, http.MethodDelete, srv.BaseURL+"/api/v1/tokens/"+tokenID, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke token status=%d body=%s", status, string(body))
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, srv.BaseURL+"/api/v1/tokens", nil)
	if status != http.StatusOK {
		t.Fatalf("list tokens after revoke status=%d body=%s", status, string(body))
	}
	tokens = testutil.DecodeJSON[[]map[string]any](t, body)
	if len(tokens) != 0 {
		t.Fatalf("expected tokens to be empty after revoke, got %+v", tokens)
	}
}

func functionalSmokeConfig(dir string, port int) string {
	return fmt.Sprintf(`auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://127.0.0.1:%d/auth/callback
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: http://127.0.0.1:%d
  dev_mode: true
  encryption_key: functional-smoke-key
`, port, filepath.Join(dir, "gestalt.db"), port, port)
}
