package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/internal/testutil"
)

func TestGestaltdProcess_LoginConnectAndInvoke(t *testing.T) {
	t.Parallel()

	oidc := testutil.NewOAuthServer(t, testutil.OAuthServerOptions{
		Email:       "process-test@gestalt.dev",
		DisplayName: "Process Test",
	})
	api := testutil.NewOpenAPIFixture(t, testutil.OpenAPIFixtureOptions{
		ExpectedBearerToken: "manual-token",
	})

	port := testutil.FreePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	dir := t.TempDir()
	configPath := testutil.WriteConfigFile(t, dir, fmt.Sprintf(`auth:
  provider: oidc
  config:
    issuer_url: %s
    client_id: test-client
    client_secret: test-secret
    redirect_url: %s/api/v1/auth/login/callback
    session_secret: session-secret
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: %s
  dev_mode: true
  encryption_key: encryption-secret
integrations:
  warehouse:
    display_name: Warehouse
    description: Warehouse test integration
    manual_auth: true
    upstreams:
      - type: rest
        url: %s
`, oidc.URL, baseURL, filepath.Join(dir, "gestalt.db"), port, baseURL, api.SpecURL))

	proc := testutil.StartGestaltd(t, configPath, port)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	loginResp := postJSON(t, client, proc.BaseURL+"/api/v1/auth/login", map[string]any{
		"state": "login-state",
	})
	defer func() { _ = loginResp.Body.Close() }()

	var loginBody struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginBody.URL == "" {
		t.Fatal("expected login URL")
	}

	callbackResp, err := client.Get(loginBody.URL)
	if err != nil {
		t.Fatalf("follow login URL: %v", err)
	}
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusOK {
		t.Fatalf("login callback status = %d, want 200", callbackResp.StatusCode)
	}

	connectResp := postJSON(t, client, proc.BaseURL+"/api/v1/auth/connect-manual", map[string]any{
		"integration": "warehouse",
		"credential":  "manual-token",
	})
	defer func() { _ = connectResp.Body.Close() }()

	var connectBody struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(connectResp.Body).Decode(&connectBody); err != nil {
		t.Fatalf("decode connect response: %v", err)
	}
	if connectBody.Status != "connected" {
		t.Fatalf("connect status = %q, want %q", connectBody.Status, "connected")
	}

	invokeResp, err := client.Get(proc.BaseURL + "/api/v1/warehouse/list_items")
	if err != nil {
		t.Fatalf("invoke request: %v", err)
	}
	defer func() { _ = invokeResp.Body.Close() }()
	if invokeResp.StatusCode != http.StatusOK {
		t.Fatalf("invoke status = %d, want 200", invokeResp.StatusCode)
	}

	var invokeBody map[string]any
	if err := json.NewDecoder(invokeResp.Body).Decode(&invokeBody); err != nil {
		t.Fatalf("decode invoke response: %v", err)
	}
	items, ok := invokeBody["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("invoke items = %#v, want 2 items", invokeBody["items"])
	}

	if auth := api.LastAuthorization(); auth != "Bearer manual-token" {
		t.Fatalf("upstream Authorization = %q, want %q", auth, "Bearer manual-token")
	}
}

func TestGestaltdProcess_DevLoginAndTokenCRUD(t *testing.T) {
	t.Parallel()

	port := testutil.FreePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	dir := t.TempDir()
	configPath := testutil.WriteConfigFile(t, dir, fmt.Sprintf(`auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: %s/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: %s
  dev_mode: true
  encryption_key: encryption-secret
`, baseURL, filepath.Join(dir, "gestalt.db"), port, baseURL))

	proc := testutil.StartGestaltd(t, configPath, port)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	devLoginResp := postJSON(t, client, proc.BaseURL+"/api/dev-login", map[string]any{
		"email": "dev-login@gestalt.dev",
	})
	defer func() { _ = devLoginResp.Body.Close() }()

	createResp := postJSON(t, client, proc.BaseURL+"/api/v1/tokens", map[string]any{
		"name": "process-token",
	})
	defer func() { _ = createResp.Body.Close() }()

	var created struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create token response: %v", err)
	}
	if created.Name != "process-token" || created.Token == "" {
		t.Fatalf("unexpected created token payload: %+v", created)
	}

	listResp, err := client.Get(proc.BaseURL + "/api/v1/tokens")
	if err != nil {
		t.Fatalf("list tokens request: %v", err)
	}
	defer func() { _ = listResp.Body.Close() }()

	var listed []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list tokens response: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "process-token" {
		t.Fatalf("listed tokens = %+v, want one process-token", listed)
	}

	req, err := http.NewRequest(http.MethodDelete, proc.BaseURL+"/api/v1/tokens/"+created.ID, nil)
	if err != nil {
		t.Fatalf("new revoke request: %v", err)
	}
	revokeResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("revoke token request: %v", err)
	}
	defer func() { _ = revokeResp.Body.Close() }()
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", revokeResp.StatusCode)
	}
}

func postJSON(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	if resp.StatusCode >= 400 {
		t.Fatalf("POST %s returned %d", url, resp.StatusCode)
	}
	return resp
}
