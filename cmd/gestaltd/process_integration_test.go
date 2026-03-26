package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/internal/testutil"
)

func TestGestaltdProcess_PrepareServeOffline(t *testing.T) {
	t.Parallel()

	port := testutil.FreePort(t)
	dir := t.TempDir()
	openAPI := testutil.NewOpenAPIServer(t, "")
	cfgPath := writeProcessConfig(t, dir, fmt.Sprintf(`
auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://127.0.0.1:%d/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: http://127.0.0.1:%d
  dev_mode: true
  encryption_key: test-key
integrations:
  restapi:
    display_name: REST API
    upstreams:
      - type: rest
        url: %s
`, port, filepath.Join(dir, "gestalt.db"), port, port, openAPI.URL))

	if err := prepareConfig(cfgPath); err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}
	openAPI.Close()

	proc := testutil.StartGestaltd(t, cfgPath, port)
	client := testutil.NewCookieClient(t)
	testutil.DevLogin(t, client, proc.BaseURL, "dev@example.com")

	status, body := testutil.DoJSON(t, client, http.MethodGet, proc.BaseURL+"/api/v1/integrations/restapi/operations", nil)
	if status != http.StatusOK {
		t.Fatalf("operations status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}
	var ops []map[string]any
	if err := json.Unmarshal(body, &ops); err != nil {
		t.Fatalf("decode operations: %v", err)
	}
	if len(ops) == 0 || ops[0]["Name"] != "list_items" {
		t.Fatalf("expected prepared list_items operation, got %v", ops)
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, proc.BaseURL+"/api/v1/integrations", nil)
	if status != http.StatusOK {
		t.Fatalf("integrations status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}
	if !bytes.Contains(body, []byte(`"restapi"`)) {
		t.Fatalf("expected restapi integration in list, got %s", string(body))
	}
}

func TestGestaltdProcess_StagedConnectionSelection(t *testing.T) {
	t.Parallel()

	port := testutil.FreePort(t)
	dir := t.TempDir()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	openAPI := testutil.NewOpenAPIServer(t, "")
	oauth := testutil.NewOAuthServer(t, testutil.OAuthServerOptions{})
	discovery := testutil.NewDiscoveryServer(t, testutil.DiscoveryServerOptions{
		Candidates: []map[string]any{
			{"id": "acct-a", "name": "Alpha", "region": "us-east-1", "workspace_id": "alpha"},
			{"id": "acct-b", "name": "Beta", "region": "us-west-2", "workspace_id": "beta"},
		},
	})
	cfgPath := writeProcessConfig(t, dir, fmt.Sprintf(`
auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://127.0.0.1:%d/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: %s
  dev_mode: true
  encryption_key: test-key
integrations:
  stagedsvc:
    display_name: Staged Service
    auth:
      type: oauth2
      authorization_url: %s/authorize
      token_url: %s/token
    client_id: staged-client
    client_secret: staged-secret
    upstreams:
      - type: rest
        url: %s
`, port, filepath.Join(dir, "gestalt.db"), port, baseURL, oauth.URL, oauth.URL, openAPI.URL))

	if err := prepareConfig(cfgPath); err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}
	patchPreparedProviderDiscovery(t, cfgPath, "stagedsvc", discovery.URL)
	openAPI.Close()

	proc := testutil.StartGestaltd(t, cfgPath, port)
	client := testutil.NewCookieClient(t)
	testutil.DevLogin(t, client, proc.BaseURL, "dev@example.com")

	startStatus, startBody := testutil.DoJSON(t, client, http.MethodPost, proc.BaseURL+"/api/v1/auth/start-oauth", map[string]any{
		"integration": "stagedsvc",
		"scopes":      []string{},
	})
	if startStatus != http.StatusOK {
		t.Fatalf("start oauth status=%d body=%s\nlogs:\n%s", startStatus, string(startBody), proc.Logs())
	}
	startResp := testutil.DecodeJSON[map[string]string](t, startBody)
	authURL := startResp["url"]
	if authURL == "" {
		t.Fatal("missing oauth url")
	}

	callbackResp := followOAuthFlowToCallback(t, client, authURL, baseURL)
	defer func() { _ = callbackResp.Body.Close() }()
	if callbackResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback status=%d\nlogs:\n%s", callbackResp.StatusCode, proc.Logs())
	}
	location := callbackResp.Header.Get("Location")
	if !strings.Contains(location, "/integrations?pending=") {
		t.Fatalf("expected staged redirect, got %q", location)
	}
	stagedID := queryParam(t, location, "pending")

	status, body := testutil.DoJSON(t, client, http.MethodGet, proc.BaseURL+"/api/v1/connections/staged/"+stagedID, nil)
	if status != http.StatusOK {
		t.Fatalf("staged connection status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}
	var staged map[string]any
	if err := json.Unmarshal(body, &staged); err != nil {
		t.Fatalf("decode staged connection: %v", err)
	}
	candidates, ok := staged["candidates"].([]any)
	if !ok || len(candidates) != 2 {
		t.Fatalf("expected 2 staged candidates, got %v", staged["candidates"])
	}
	firstCandidate := candidates[0].(map[string]any)
	candidateID, _ := firstCandidate["id"].(string)
	if candidateID == "" {
		t.Fatalf("missing candidate id in %v", firstCandidate)
	}

	status, body = testutil.DoJSON(t, client, http.MethodPost, proc.BaseURL+"/api/v1/connections/staged/"+stagedID+"/select", map[string]string{
		"candidate_id": candidateID,
	})
	if status != http.StatusOK {
		t.Fatalf("select staged status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}
	selected := testutil.DecodeJSON[map[string]string](t, body)
	if selected["status"] != "connected" {
		t.Fatalf("expected connected status, got %v", selected)
	}

	status, body = testutil.DoJSON(t, client, http.MethodGet, proc.BaseURL+"/api/v1/integrations", nil)
	if status != http.StatusOK {
		t.Fatalf("integrations status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}
	if !bytes.Contains(body, []byte(`"stagedsvc"`)) {
		t.Fatalf("expected stagedsvc to appear in integrations list, got %s", string(body))
	}
}

func TestGestaltdProcess_MCPAndBindings(t *testing.T) {
	t.Parallel()

	port := testutil.FreePort(t)
	dir := t.TempDir()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	mcpUp := testutil.NewMCPServer(t)
	proxyUp := testutil.NewOpenAPIServer(t, "proxy-token")
	cfgPath := writeProcessConfig(t, dir, fmt.Sprintf(`
auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://127.0.0.1:%d/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: %s
server:
  port: %d
  base_url: %s
  dev_mode: true
  encryption_key: test-key
integrations:
  mcpsvc:
    display_name: MCP Service
    auth:
      type: manual
    manual_auth: true
    upstreams:
      - type: mcp
        url: %s
  proxysvc:
    display_name: Proxy Service
    auth:
      type: manual
    manual_auth: true
    upstreams:
      - type: rest
        url: %s
bindings:
  echo-webhook:
    type: webhook
    config:
      path: /incoming
  proxy-route:
    type: proxy
    providers:
      - proxysvc
    config:
      path: /proxy
`, port, filepath.Join(dir, "gestalt.db"), port, baseURL, mcpUp.URL, proxyUp.URL))

	proc := testutil.StartGestaltd(t, cfgPath, port)
	client := testutil.NewCookieClient(t)
	testutil.DevLogin(t, client, proc.BaseURL, "dev@example.com")

	status, body := testutil.DoJSON(t, client, http.MethodPost, proc.BaseURL+"/api/v1/auth/connect-manual", map[string]any{
		"integration": "mcpsvc",
		"credential":  "mcp-token",
	})
	if status != http.StatusOK {
		t.Fatalf("connect mcp manual status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}

	waitForMCPMounted(t, client, proc.BaseURL)

	status, resp := doMCPJSONRPC(t, client, proc.BaseURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d\nresp=%v\nlogs:\n%s", status, resp, proc.Logs())
	}
	status, resp = doMCPJSONRPC(t, client, proc.BaseURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d\nresp=%v\nlogs:\n%s", status, resp, proc.Logs())
	}
	result, _ := resp["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("expected MCP tools, got %v", result)
	}
	foundEcho := false
	for _, tool := range tools {
		m := tool.(map[string]any)
		if m["name"] == "mcpsvc_echo" {
			foundEcho = true
			break
		}
	}
	if !foundEcho {
		t.Fatalf("expected mcpsvc_echo in tools list, got %v", tools)
	}

	status, resp = doMCPJSONRPC(t, client, proc.BaseURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "mcpsvc_echo",
			"arguments": map[string]any{"message": "hello"},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call: expected 200, got %d\nresp=%v\nlogs:\n%s", status, resp, proc.Logs())
	}
	result, _ = resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("expected MCP call content, got %v", result)
	}

	status, body = testutil.DoJSON(t, client, http.MethodPost, proc.BaseURL+"/api/v1/bindings/echo-webhook/incoming", map[string]any{
		"ok": true,
	})
	if status != http.StatusOK {
		t.Fatalf("webhook binding status=%d body=%s\nlogs:\n%s", status, string(body), proc.Logs())
	}
	if !bytes.Contains(body, []byte(`"ok":true`)) {
		t.Fatalf("expected webhook binding to echo body, got %s", string(body))
	}
}

func writeProcessConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func patchPreparedProviderDiscovery(t *testing.T, cfgPath, integration, discoveryURL string) {
	t.Helper()

	providerPath := filepath.Join(filepath.Dir(cfgPath), preparedProvidersDir, integration+".json")
	data, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read prepared provider: %v", err)
	}

	var def map[string]any
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("decode prepared provider: %v", err)
	}
	def["post_connect_discovery"] = map[string]any{
		"url":        discoveryURL,
		"items_path": "accounts",
		"id_path":    "id",
		"name_path":  "name",
		"metadata_mapping": map[string]string{
			"region":       "region",
			"workspace_id": "workspace_id",
		},
	}

	updated, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("encode prepared provider: %v", err)
	}
	if err := os.WriteFile(providerPath, updated, 0o644); err != nil {
		t.Fatalf("write prepared provider: %v", err)
	}
}

func followOAuthFlowToCallback(t *testing.T, client *http.Client, authURL, baseURL string) *http.Response {
	t.Helper()

	base, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}

	oldRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host == base.Host && req.URL.Path == "/api/v1/auth/callback" {
			return nil
		}
		if req.URL.Host == base.Host && req.URL.Path == "/integrations" {
			return http.ErrUseLastResponse
		}
		return nil
	}
	defer func() { client.CheckRedirect = oldRedirect }()

	req, err := http.NewRequest(http.MethodGet, authURL, nil)
	if err != nil {
		t.Fatalf("new oauth request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("follow oauth flow: %v", err)
	}
	return resp
}

func queryParam(t *testing.T, rawURL, key string) string {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return parsed.Query().Get(key)
}

func doMCPJSONRPC(t *testing.T, client *http.Client, baseURL string, body map[string]any) (int, map[string]any) {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal mcp request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new mcp request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("mcp request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read mcp response: %v", err)
	}
	var result map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatalf("decode mcp response: %v\nbody=%s", err, string(raw))
		}
	}
	return resp.StatusCode, result
}

func waitForMCPMounted(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := doMCPJSONRPC(t, client, baseURL, map[string]any{
			"jsonrpc": "2.0",
			"id":      99,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "probe", "version": "1.0"},
			},
		})
		if status != http.StatusNotFound {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("timed out waiting for MCP endpoint to mount")
}
