package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestE2EValidateRejectsAuditConfigWhenProviderInheritsTelemetry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	cfgPath := writeE2EConfig(t, dir, pluginDir, 18080)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`audit:
  config:
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config audit: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
	}
	if !strings.Contains(string(out), "audit.config is not supported when audit.provider is") {
		t.Fatalf("expected inherit-provider audit config error, got: %s", out)
	}
}

func TestE2EValidateRejectsInvalidAuditSettings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		auditYAML string
		wantError string
	}{
		{
			name: "unknown audit provider",
			auditYAML: `audit:
  provider: bogus
`,
			wantError: "unknown audit.provider",
		},
		{
			name: "stdout audit requires mapping config",
			auditYAML: `audit:
  provider: stdout
  config: nope
`,
			wantError: "stdout audit: parsing config",
		},
		{
			name: "otlp audit rejects non-otlp logs exporter",
			auditYAML: `audit:
  provider: otlp
  config:
    logs:
      exporter: stdout
`,
			wantError: "otlp audit: logs.exporter must be",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pluginDir := setupPluginDir(t, dir)

			cfgPath := writeE2EConfig(t, dir, pluginDir, 18080)
			cfgBytes, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatalf("read config: %v", err)
			}
			cfgBytes = append(cfgBytes, []byte(tc.auditYAML)...)
			if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
				t.Fatalf("write config audit: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected gestaltd validate to fail, got success\n%s", out)
			}
			if !strings.Contains(string(out), tc.wantError) {
				t.Fatalf("expected %q, got: %s", tc.wantError, out)
			}
		})
	}
}

func TestE2EDeclarativeSelectorsSupportHeaderPaginationAndResponseMapping(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := allocateTestPort(t)

	var (
		mu              sync.Mutex
		paginationCalls []string
	)
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/paginated-items":
			cursor := r.URL.Query().Get("after_cursor")
			mu.Lock()
			paginationCalls = append(paginationCalls, cursor)
			mu.Unlock()
			if cursor == "" {
				w.Header().Set("X-After-Cursor", "cursor-2")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []any{
						map[string]any{"id": "1"},
						map[string]any{"id": "2"},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{
					map[string]any{"id": "3"},
				},
			})
		case "/mapped-items":
			w.Header().Set("X-After-Cursor", "cursor-map")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []any{
					map[string]any{"id": "7"},
				},
				"moreDataAvailable": true,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	testutil.CloseOnCleanup(t, apiSrv)

	pagerDir := filepath.Join(dir, "pager-plugin")
	if err := os.MkdirAll(pagerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll pager plugin: %v", err)
	}
	writeTestFile(t, pagerDir, "openapi.yaml", []byte(fmt.Sprintf(`openapi: 3.0.0
info:
  title: Pager
  version: 1.0.0
servers:
  - url: %s
paths:
  /paginated-items:
    get:
      operationId: list_items
      responses:
        '200':
          description: ok
`, apiSrv.URL)), 0o644)
	writeTestFile(t, pagerDir, "manifest.yaml", []byte(`
source: github.com/test/plugins/pager
version: 0.0.1-alpha.1
displayName: Pager
plugin:
  surfaces:
    openapi:
      document: openapi.yaml
  connectionMode: none
  pagination:
    style: cursor
    cursorParam: after_cursor
    cursor:
      source: header
      path: X-After-Cursor
    resultsPath: data
  allowedOperations:
    list_items:
      paginate: true
`), 0o644)
	pagerManifest, err := pluginpkg.FindManifestFile(pagerDir)
	if err != nil {
		t.Fatalf("FindManifestFile pager: %v", err)
	}

	mapperDir := filepath.Join(dir, "mapper-plugin")
	if err := os.MkdirAll(mapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll mapper plugin: %v", err)
	}
	writeTestFile(t, mapperDir, "openapi.yaml", []byte(fmt.Sprintf(`openapi: 3.0.0
info:
  title: Mapper
  version: 1.0.0
servers:
  - url: %s
paths:
  /mapped-items:
    get:
      operationId: list_items
      responses:
        '200':
          description: ok
`, apiSrv.URL)), 0o644)
	writeTestFile(t, mapperDir, "manifest.yaml", []byte(`
source: github.com/test/plugins/mapper
version: 0.0.1-alpha.1
displayName: Mapper
plugin:
  surfaces:
    openapi:
      document: openapi.yaml
  connectionMode: none
  responseMapping:
    dataPath: results
    pagination:
      hasMore:
        source: body
        path: moreDataAvailable
      cursor:
        source: header
        path: X-After-Cursor
`), 0o644)
	mapperManifest, err := pluginpkg.FindManifestFile(mapperDir)
	if err != nil {
		t.Fatalf("FindManifestFile mapper: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := authDatastoreConfigYAML(t, dir, "", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
plugins:
  pager:
    provider:
      source:
        path: %s
  mapper:
    provider:
      source:
        path: %s
`, port, pagerManifest, mapperManifest)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		pagerReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/pager/list_items", strings.NewReader(`{}`))
		pagerReq.Header.Set("Content-Type", "application/json")
		pagerResp, err := http.DefaultClient.Do(pagerReq)
		if err != nil {
			t.Fatalf("invoke pager: %v", err)
		}
		defer func() { _ = pagerResp.Body.Close() }()
		if pagerResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(pagerResp.Body)
			t.Fatalf("pager status = %d: %s", pagerResp.StatusCode, body)
		}

		var pagerItems []map[string]any
		if err := json.NewDecoder(pagerResp.Body).Decode(&pagerItems); err != nil {
			t.Fatalf("decode pager response: %v", err)
		}
		if len(pagerItems) != 3 {
			t.Fatalf("pager item count = %d, want 3", len(pagerItems))
		}

		mu.Lock()
		gotCalls := append([]string(nil), paginationCalls...)
		mu.Unlock()
		if len(gotCalls) != 2 || gotCalls[0] != "" || gotCalls[1] != "cursor-2" {
			t.Fatalf("pagination calls = %v, want [\"\", \"cursor-2\"]", gotCalls)
		}

		mapperReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/mapper/list_items", strings.NewReader(`{}`))
		mapperReq.Header.Set("Content-Type", "application/json")
		mapperResp, err := http.DefaultClient.Do(mapperReq)
		if err != nil {
			t.Fatalf("invoke mapper: %v", err)
		}
		defer func() { _ = mapperResp.Body.Close() }()
		if mapperResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(mapperResp.Body)
			t.Fatalf("mapper status = %d: %s", mapperResp.StatusCode, body)
		}

		var mapped struct {
			Data       []map[string]any `json:"data"`
			Pagination struct {
				HasMore bool   `json:"hasMore"`
				Cursor  string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := json.NewDecoder(mapperResp.Body).Decode(&mapped); err != nil {
			t.Fatalf("decode mapper response: %v", err)
		}
		if len(mapped.Data) != 1 || mapped.Data[0]["id"] != "7" {
			t.Fatalf("mapped data = %+v, want single id=7 item", mapped.Data)
		}
		if !mapped.Pagination.HasMore || mapped.Pagination.Cursor != "cursor-map" {
			t.Fatalf("mapped pagination = %+v", mapped.Pagination)
		}
	})
}

func TestE2EManualAuthMappingValueAndValueFromBasicAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := allocateTestPort(t)

	var upstreamAuth atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth":        r.Header.Get("Authorization"),
			"org_header":  r.Header.Get("X-Org-ID"),
			"echo_header": r.Header.Get("X-API-Key-Echo"),
		})
	}))
	testutil.CloseOnCleanup(t, upstream)

	pluginDir := filepath.Join(dir, "manual-basic-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll plugin dir: %v", err)
	}
	writeTestFile(t, pluginDir, "openapi.yaml", []byte(fmt.Sprintf(`openapi: 3.0.0
info:
  title: Manual Basic Test
  version: 1.0.0
servers:
  - url: %s
components:
  securitySchemes:
    basic_auth:
      type: http
      scheme: basic
security:
  - basic_auth: []
paths:
  /whoami:
    get:
      operationId: whoami
      responses:
        '200':
          description: ok
`, upstream.URL)), 0o644)
	writeTestFile(t, pluginDir, "manifest.yaml", []byte(`
source: github.com/test/plugins/manual-basic
version: 0.0.1-alpha.1
displayName: Manual Basic Test
plugin:
  surfaces:
    openapi:
      document: openapi.yaml
`), 0o644)

	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := authDatastoreConfigYAML(t, dir, "session-auth", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
plugins:
  mt:
    provider:
      source:
        path: %s
    connections:
      default:
        auth:
          type: manual
          credentials:
            - name: api_key
              label: API Key
          authMapping:
            headers:
              X-Org-ID:
                value: org-fixed
              X-API-Key-Echo:
                valueFrom:
                  credentialFieldRef:
                    name: api_key
            basic:
              username:
                value: org-fixed
              password:
                valueFrom:
                  credentialFieldRef:
                    name: api_key
`, port, manifestPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		listReq, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/integrations", nil)
		listReq.Header.Set("Authorization", "Bearer alice")
		listResp, err := http.DefaultClient.Do(listReq)
		if err != nil {
			t.Fatalf("list integrations: %v", err)
		}
		defer func() { _ = listResp.Body.Close() }()
		if listResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(listResp.Body)
			t.Fatalf("list integrations status = %d: %s", listResp.StatusCode, body)
		}

		var integrations []struct {
			Name        string `json:"name"`
			Connections []struct {
				CredentialFields []struct {
					Name string `json:"name"`
				} `json:"credentialFields"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(listResp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decode integrations: %v", err)
		}
		if len(integrations) != 1 || integrations[0].Name != "mt" {
			t.Fatalf("unexpected integrations payload: %+v", integrations)
		}
		if len(integrations[0].Connections) != 1 {
			t.Fatalf("unexpected connections payload: %+v", integrations[0].Connections)
		}
		fields := integrations[0].Connections[0].CredentialFields
		if len(fields) != 1 || fields[0].Name != "api_key" {
			t.Fatalf("credential fields = %+v, want only api_key", fields)
		}

		connectReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/connect-manual", strings.NewReader(`{"integration":"mt","credential":"api-key-123"}`))
		connectReq.Header.Set("Authorization", "Bearer alice")
		connectReq.Header.Set("Content-Type", "application/json")
		connectResp, err := http.DefaultClient.Do(connectReq)
		if err != nil {
			t.Fatalf("connect manual: %v", err)
		}
		defer func() { _ = connectResp.Body.Close() }()
		if connectResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(connectResp.Body)
			t.Fatalf("connect manual status = %d: %s", connectResp.StatusCode, body)
		}

		invokeReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/mt/whoami", strings.NewReader(`{}`))
		invokeReq.Header.Set("Authorization", "Bearer alice")
		invokeReq.Header.Set("Content-Type", "application/json")
		invokeResp, err := http.DefaultClient.Do(invokeReq)
		if err != nil {
			t.Fatalf("invoke whoami: %v", err)
		}
		defer func() { _ = invokeResp.Body.Close() }()
		if invokeResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(invokeResp.Body)
			t.Fatalf("invoke whoami status = %d: %s", invokeResp.StatusCode, body)
		}

		var result map[string]any
		if err := json.NewDecoder(invokeResp.Body).Decode(&result); err != nil {
			t.Fatalf("decode invoke response: %v", err)
		}

		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("org-fixed:api-key-123"))
		if result["auth"] != wantAuth {
			t.Fatalf("invoke auth = %v, want %v", result["auth"], wantAuth)
		}
		if result["org_header"] != "org-fixed" {
			t.Fatalf("invoke org_header = %v, want org-fixed", result["org_header"])
		}
		if result["echo_header"] != "api-key-123" {
			t.Fatalf("invoke echo_header = %v, want api-key-123", result["echo_header"])
		}
		if got, _ := upstreamAuth.Load().(string); got != wantAuth {
			t.Fatalf("upstream auth = %q, want %q", got, wantAuth)
		}
	})
}

//nolint:paralleltest // Spawns gestaltd and is flaky under package-wide parallel load in CI.
func TestE2EManifestManualAuthMappingValueFromBasicAuth(t *testing.T) {
	dir := t.TempDir()
	port := allocateTestPort(t)

	var upstreamAuth atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth":      r.Header.Get("Authorization"),
			"x_app_key": r.Header.Get("X-App-Key"),
		})
	}))
	testutil.CloseOnCleanup(t, upstream)

	pluginDir := filepath.Join(dir, "manifest-basic-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll plugin dir: %v", err)
	}
	writeTestFile(t, pluginDir, "openapi.yaml", []byte(fmt.Sprintf(`openapi: 3.0.0
info:
  title: Manifest Basic Test
  version: 1.0.0
servers:
  - url: %s
components:
  securitySchemes:
    basic_auth:
      type: http
      scheme: basic
security:
  - basic_auth: []
paths:
  /whoami:
    get:
      operationId: whoami
      responses:
        '200':
          description: ok
`, upstream.URL)), 0o644)
	writeTestFile(t, pluginDir, "manifest.yaml", []byte(`
source: github.com/test/plugins/manifest-basic
version: 0.0.1-alpha.1
displayName: Manifest Basic Test
plugin:
  auth:
    type: manual
    credentials:
      - name: organization_id
        label: Organization ID
      - name: api_key
        label: API Key
      - name: app_key
        label: App Key
    authMapping:
      headers:
        X-App-Key:
          valueFrom:
            credentialFieldRef:
              name: app_key
      basic:
        username:
          valueFrom:
            credentialFieldRef:
              name: organization_id
        password:
          valueFrom:
            credentialFieldRef:
              name: api_key
  surfaces:
    openapi:
      document: openapi.yaml
`), 0o644)

	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := authDatastoreConfigYAML(t, dir, "session-auth", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
plugins:
  mt:
    provider:
      source:
        path: %s
`, port, manifestPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		listReq, _ := http.NewRequest(http.MethodGet, baseURL+"/api/v1/integrations", nil)
		listReq.Header.Set("Authorization", "Bearer alice")
		listResp, err := http.DefaultClient.Do(listReq)
		if err != nil {
			t.Fatalf("list integrations: %v", err)
		}
		defer func() { _ = listResp.Body.Close() }()
		if listResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(listResp.Body)
			t.Fatalf("list integrations status = %d: %s", listResp.StatusCode, body)
		}

		var integrations []struct {
			Name        string `json:"name"`
			Connections []struct {
				CredentialFields []struct {
					Name string `json:"name"`
				} `json:"credentialFields"`
			} `json:"connections"`
		}
		if err := json.NewDecoder(listResp.Body).Decode(&integrations); err != nil {
			t.Fatalf("decode integrations: %v", err)
		}
		if len(integrations) != 1 || integrations[0].Name != "mt" {
			t.Fatalf("unexpected integrations payload: %+v", integrations)
		}
		fields := integrations[0].Connections[0].CredentialFields
		if len(fields) != 3 || fields[0].Name != "organization_id" || fields[1].Name != "api_key" || fields[2].Name != "app_key" {
			t.Fatalf("credential fields = %+v, want organization_id, api_key, and app_key", fields)
		}

		connectReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/connect-manual", strings.NewReader(`{"integration":"mt","credentials":{"organization_id":"org-123","api_key":"api-key-123","app_key":"app-key-123"}}`))
		connectReq.Header.Set("Authorization", "Bearer alice")
		connectReq.Header.Set("Content-Type", "application/json")
		connectResp, err := http.DefaultClient.Do(connectReq)
		if err != nil {
			t.Fatalf("connect manual: %v", err)
		}
		defer func() { _ = connectResp.Body.Close() }()
		if connectResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(connectResp.Body)
			t.Fatalf("connect manual status = %d: %s", connectResp.StatusCode, body)
		}

		invokeReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/mt/whoami", strings.NewReader(`{}`))
		invokeReq.Header.Set("Authorization", "Bearer alice")
		invokeReq.Header.Set("Content-Type", "application/json")
		invokeResp, err := http.DefaultClient.Do(invokeReq)
		if err != nil {
			t.Fatalf("invoke whoami: %v", err)
		}
		defer func() { _ = invokeResp.Body.Close() }()
		if invokeResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(invokeResp.Body)
			t.Fatalf("invoke whoami status = %d: %s", invokeResp.StatusCode, body)
		}

		var result map[string]any
		if err := json.NewDecoder(invokeResp.Body).Decode(&result); err != nil {
			t.Fatalf("decode invoke response: %v", err)
		}

		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("org-123:api-key-123"))
		if result["auth"] != wantAuth {
			t.Fatalf("invoke auth = %v, want %v", result["auth"], wantAuth)
		}
		if result["x_app_key"] != "app-key-123" {
			t.Fatalf("invoke x_app_key = %v, want %v", result["x_app_key"], "app-key-123")
		}
		if got, _ := upstreamAuth.Load().(string); got != wantAuth {
			t.Fatalf("upstream auth = %q, want %q", got, wantAuth)
		}
	})
}

//nolint:paralleltest // Spawns gestaltd and is flaky under package-wide parallel load in CI.
func TestE2EDeclarativeManifestManualAuthMappingValueFromBasicAuth(t *testing.T) {
	dir := t.TempDir()
	port := allocateTestPort(t)

	var upstreamAuth atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth":      r.Header.Get("Authorization"),
			"x_app_key": r.Header.Get("X-App-Key"),
		})
	}))
	testutil.CloseOnCleanup(t, upstream)

	pluginDir := filepath.Join(dir, "declarative-basic-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll plugin dir: %v", err)
	}
	writeTestFile(t, pluginDir, "manifest.yaml", []byte(fmt.Sprintf(`
source: github.com/test/plugins/declarative-basic
version: 0.0.1-alpha.1
displayName: Declarative Basic Test
plugin:
  auth:
    type: manual
    credentials:
      - name: organization_id
        label: Organization ID
      - name: api_key
        label: API Key
      - name: app_key
        label: App Key
    authMapping:
      headers:
        X-App-Key:
          valueFrom:
            credentialFieldRef:
              name: app_key
      basic:
        username:
          valueFrom:
            credentialFieldRef:
              name: organization_id
        password:
          valueFrom:
            credentialFieldRef:
              name: api_key
  surfaces:
    rest:
      baseUrl: %s
      operations:
        - name: whoami
          method: GET
          path: /whoami
`, upstream.URL)), 0o644)

	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := authDatastoreConfigYAML(t, dir, "session-auth", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
plugins:
  mt:
    provider:
      source:
        path: %s
`, port, manifestPath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		connectReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/connect-manual", strings.NewReader(`{"integration":"mt","credentials":{"organization_id":"org-456","api_key":"api-key-456","app_key":"app-key-456"}}`))
		connectReq.Header.Set("Authorization", "Bearer alice")
		connectReq.Header.Set("Content-Type", "application/json")
		connectResp, err := http.DefaultClient.Do(connectReq)
		if err != nil {
			t.Fatalf("connect manual: %v", err)
		}
		defer func() { _ = connectResp.Body.Close() }()
		if connectResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(connectResp.Body)
			t.Fatalf("connect manual status = %d: %s", connectResp.StatusCode, body)
		}

		invokeReq, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/mt/whoami", strings.NewReader(`{}`))
		invokeReq.Header.Set("Authorization", "Bearer alice")
		invokeReq.Header.Set("Content-Type", "application/json")
		invokeResp, err := http.DefaultClient.Do(invokeReq)
		if err != nil {
			t.Fatalf("invoke whoami: %v", err)
		}
		defer func() { _ = invokeResp.Body.Close() }()
		if invokeResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(invokeResp.Body)
			t.Fatalf("invoke whoami status = %d: %s", invokeResp.StatusCode, body)
		}

		var result map[string]any
		if err := json.NewDecoder(invokeResp.Body).Decode(&result); err != nil {
			t.Fatalf("decode invoke response: %v", err)
		}

		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("org-456:api-key-456"))
		if result["auth"] != wantAuth {
			t.Fatalf("invoke auth = %v, want %v", result["auth"], wantAuth)
		}
		if result["x_app_key"] != "app-key-456" {
			t.Fatalf("invoke x_app_key = %v, want %v", result["x_app_key"], "app-key-456")
		}
		if got, _ := upstreamAuth.Load().(string); got != wantAuth {
			t.Fatalf("upstream auth = %q, want %q", got, wantAuth)
		}
	})
}

func TestE2EInitServeLockedGoldenPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	deployDir := filepath.Join(dir, "deploy")
	dataDir := filepath.Join(dir, "data")
	artifactsDir := filepath.Join(dir, "runtime-artifacts")
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatalf("MkdirAll deploy dir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll data dir: %v", err)
	}

	pluginDir := setupPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfigWithPaths(t, deployDir, pluginDir, filepath.Join(dataDir, "gestalt.db"), artifactsDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: stdout
  config:
    level: warn
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath, "--artifacts-dir", artifactsDir).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	lockPath := filepath.Join(deployDir, operator.InitLockfileName)
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	var rawLock map[string]any
	if err := json.Unmarshal(lockBytes, &rawLock); err != nil {
		t.Fatalf("decode lockfile json: %v", err)
	}
	if _, ok := rawLock["plugins"]; ok {
		t.Fatalf("expected lockfile to omit legacy plugins map: %s", lockBytes)
	}
	rawProviders, ok := rawLock["providers"].(map[string]any)
	if !ok {
		t.Fatalf("expected lockfile providers object: %s", lockBytes)
	}
	if len(rawProviders) != 0 {
		t.Fatalf("expected local source config to avoid prepared provider entries, got: %s", lockBytes)
	}

	lock, err := operator.ReadLockfile(lockPath)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if lock.Version != 2 {
		t.Fatalf("lock version = %d, want 2", lock.Version)
	}
	if len(lock.Providers) != 0 {
		t.Fatalf("expected no prepared provider entries for local source config, got %+v", lock.Providers)
	}
	if lock.UI != nil {
		t.Fatalf("expected no ui lock entry when config has no managed ui plugin")
	}

	t.Cleanup(func() {
		_ = os.Chmod(deployDir, 0o755)
		_ = os.Chmod(cfgPath, 0o644)
		_ = os.Chmod(lockPath, 0o644)
	})
	if err := os.Chmod(cfgPath, 0o444); err != nil {
		t.Fatalf("Chmod config: %v", err)
	}
	if err := os.Chmod(lockPath, 0o444); err != nil {
		t.Fatalf("Chmod lockfile: %v", err)
	}
	if err := os.Chmod(deployDir, 0o555); err != nil {
		t.Fatalf("Chmod deploy dir: %v", err)
	}

	stdout, stderr := serveLockedAndInvokeExampleEcho(t, cfgPath, port, artifactsDir)

	var foundAudit bool
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["msg"] != "audit" {
			continue
		}

		foundAudit = true
		if record["level"] != "INFO" {
			t.Fatalf("expected audit log level=INFO, got %v\nstdout:\n%s\nstderr:\n%s", record["level"], stdout, stderr)
		}
		if record["log.type"] != "audit" {
			t.Fatalf("expected audit log.type=audit, got %v\nstdout:\n%s\nstderr:\n%s", record["log.type"], stdout, stderr)
		}
		if record["provider"] != "example" {
			t.Fatalf("expected audit provider=example, got %v\nstdout:\n%s\nstderr:\n%s", record["provider"], stdout, stderr)
		}
		if record["operation"] != "echo" {
			t.Fatalf("expected audit operation=echo, got %v\nstdout:\n%s\nstderr:\n%s", record["operation"], stdout, stderr)
		}
		if record["allowed"] != true {
			t.Fatalf("expected audit allowed=true, got %v\nstdout:\n%s\nstderr:\n%s", record["allowed"], stdout, stderr)
		}
		break
	}

	if !foundAudit {
		t.Fatalf("expected audit log in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedOTLPExportsTracesAndMetricsButKeepsLogsOnStdout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	var logRequests, traceRequests, metricRequests atomic.Int32
	var metricBodiesMu sync.Mutex
	var metricBodies [][]byte
	otlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		switch r.URL.Path {
		case "/v1/logs":
			logRequests.Add(1)
		case "/v1/traces":
			traceRequests.Add(1)
		case "/v1/metrics":
			metricRequests.Add(1)
			metricBodiesMu.Lock()
			metricBodies = append(metricBodies, bytes.Clone(body))
			metricBodiesMu.Unlock()
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(otlpServer.Close)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: otlp
  config:
    endpoint: `+strings.TrimPrefix(otlpServer.URL, "http://")+`
    protocol: http
    insecure: true
    traces:
      samplingRatio: 1.0
    metrics:
      interval: 50ms
    logs:
      exporter: stdout
      format: json
      level: info
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		body := invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal success response: %v\nbody: %s", err, body)
		}
		if result["echo"] != "hello" {
			t.Fatalf("expected echo=hello, got %v", result)
		}

		invokeExampleOperation(t, baseURL, "nope", `{}`, http.StatusNotFound)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusOK)
		if !bytes.Contains(promBody, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("expected prometheus counter in /metrics body: %s", promBody)
		}
		if !bytes.Contains(promBody, []byte("gestaltd_operation_duration_seconds_bucket")) {
			t.Fatalf("expected prometheus histogram in /metrics body: %s", promBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected embedded admin UI at /admin: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Requests and failures")) {
			t.Fatalf("expected activity graph section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Average and p95")) {
			t.Fatalf("expected latency graph section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Top providers")) {
			t.Fatalf("expected graph sections in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Time window")) {
			t.Fatalf("expected chart controls in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Refresh cadence")) {
			t.Fatalf("expected refresh controls in embedded admin UI: %s", adminBody)
		}
		if bytes.Contains(adminBody, []byte("Raw Prometheus output")) {
			t.Fatalf("did not expect raw prometheus section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("echarts.min.js")) {
			t.Fatalf("expected admin ui to include echarts asset: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("theme.css")) {
			t.Fatalf("expected admin ui to include shared theme asset: %s", adminBody)
		}
	})

	if traceRequests.Load() == 0 {
		t.Fatalf("expected OTLP trace export\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if metricRequests.Load() == 0 {
		t.Fatalf("expected OTLP metric export\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if logRequests.Load() != 0 {
		t.Fatalf("expected logs to stay on stdout, saw %d OTLP log exports\nstdout:\n%s\nstderr:\n%s", logRequests.Load(), stdout, stderr)
	}
	metricBodiesMu.Lock()
	exports := append([][]byte(nil), metricBodies...)
	metricBodiesMu.Unlock()
	requireMetricPayload(t, exports, stdout, stderr, "gestaltd.operation.count")
	requireMetricPayload(t, exports, stdout, stderr, "gestaltd.operation.duration")
	requireMetricPayload(t, exports, stdout, stderr, "gestaltd.operation.error_count")
	requireMetricPayload(t, exports, stdout, stderr,
		"gestaltd.operation.count",
		"gestalt.connection_mode",
		"none",
		"gestalt.operation",
		"echo",
		"gestalt.provider",
		"example",
		"gestalt.transport",
		"plugin",
	)
	requireMetricPayload(t, exports, stdout, stderr,
		"gestaltd.operation.error_count",
		"gestalt.connection_mode",
		"none",
		"gestalt.operation",
		"unknown",
		"gestalt.provider",
		"example",
		"gestalt.transport",
		"unknown",
	)
	if !strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("expected audit log in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedOTLPAuditExportsAuditWithoutStdoutAuditLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	var logRequests atomic.Int32
	otlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/logs":
			logRequests.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(otlpServer.Close)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: stdout
  config:
    level: info
    format: json
audit:
  provider: otlp
  config:
    endpoint: `+strings.TrimPrefix(otlpServer.URL, "http://")+`
    protocol: http
    insecure: true
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		body := invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal success response: %v\nbody: %s", err, body)
		}
		if result["echo"] != "hello" {
			t.Fatalf("expected echo=hello, got %v", result)
		}
	})

	if logRequests.Load() == 0 {
		t.Fatalf("expected audit logs to export over OTLP\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("did not expect audit logs on stdout when audit.provider=otlp\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedStdoutExposesPrometheusAndEmbeddedAdminUIByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: stdout
  config:
    level: info
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)
		invokeExampleOperation(t, baseURL, "nope", `{}`, http.StatusNotFound)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusOK)
		if !bytes.Contains(promBody, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("expected prometheus counter in /metrics body: %s", promBody)
		}
		if !bytes.Contains(promBody, []byte(`gestalt_provider="example"`)) {
			t.Fatalf("expected provider label in /metrics body: %s", promBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected embedded admin UI at /admin: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Requests and failures")) {
			t.Fatalf("expected activity graph section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Top providers")) {
			t.Fatalf("expected provider chart section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Time window")) {
			t.Fatalf("expected chart controls in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("Refresh cadence")) {
			t.Fatalf("expected refresh controls in embedded admin UI: %s", adminBody)
		}
		if bytes.Contains(adminBody, []byte("Raw Prometheus output")) {
			t.Fatalf("did not expect raw prometheus section in embedded admin UI: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("echarts.min.js")) {
			t.Fatalf("expected admin ui to include echarts asset: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("theme.css")) {
			t.Fatalf("expected admin ui to include shared theme asset: %s", adminBody)
		}
	})

	if !strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("expected audit log in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedNoopKeepsAdminUIAndReturnsMetricsUnavailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: noop
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusServiceUnavailable)
		if !bytes.Contains(promBody, []byte("Prometheus metrics are unavailable")) {
			t.Fatalf("expected disabled metrics message in /metrics body: %s", promBody)
		}

		adminBody := getEndpointBody(t, baseURL+"/admin", http.StatusOK)
		if !bytes.Contains(adminBody, []byte("Prometheus metrics")) {
			t.Fatalf("expected embedded admin UI at /admin: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("echarts.min.js")) {
			t.Fatalf("expected admin ui to include echarts asset: %s", adminBody)
		}
		if !bytes.Contains(adminBody, []byte("theme.css")) {
			t.Fatalf("expected admin ui to include shared theme asset: %s", adminBody)
		}
	})
}

func TestE2EInitServeLockedNoopTelemetryWithStdoutAuditStillEmitsAuditLogs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: noop
audit:
  provider: stdout
  config:
    format: json
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		promBody := getEndpointBody(t, baseURL+"/metrics", http.StatusServiceUnavailable)
		if !bytes.Contains(promBody, []byte("Prometheus metrics are unavailable")) {
			t.Fatalf("expected disabled metrics message in /metrics body: %s", promBody)
		}
	})

	if !strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("expected audit log on stdout even with telemetry.provider=noop\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedStdoutAuditHonorsConfiguredLevel(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	port := allocateTestPort(t)
	cfgPath := writeE2EConfig(t, dir, pluginDir, port)
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgBytes = append(cfgBytes, []byte(`telemetry:
  provider: noop
audit:
  provider: stdout
  config:
    format: json
    level: warn
`)...)
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		t.Fatalf("write config telemetry: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseExample(t, cfgPath, port, "", func(t *testing.T, baseURL string) {
		invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)
	})

	if strings.Contains(stdout, `"operation":"echo"`) {
		t.Fatalf("did not expect info-level audit log at audit.level=warn\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Contains(stdout, `"msg":"audit"`) {
		t.Fatalf("did not expect any audit log on stdout for info-only traffic at audit.level=warn\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EInitServeLockedSplitManagementListener(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	publicPort := allocateTestPort(t)
	managementPort := allocateTestPort(t)
	cfgPath := writeSplitListenerE2EConfig(t, dir, pluginDir, publicPort, managementPort)

	out, err := exec.Command(gestaltdBin, "init", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("gestaltd init: %v\n%s", err, out)
	}

	stdout, stderr := serveLockedAndExerciseWithManagement(t, cfgPath, publicPort, managementPort, "", func(t *testing.T, publicBaseURL, managementBaseURL string) {
		invokeExampleOperation(t, publicBaseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		publicMetrics := getEndpointBody(t, publicBaseURL+"/metrics", http.StatusNotFound)
		if bytes.Contains(publicMetrics, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("did not expect public listener to expose /metrics: %s", publicMetrics)
		}

		publicAdmin := getEndpointBody(t, publicBaseURL+"/admin", http.StatusNotFound)
		if bytes.Contains(publicAdmin, []byte("Prometheus metrics")) {
			t.Fatalf("did not expect public listener to expose /admin: %s", publicAdmin)
		}

		managementMetrics := getEndpointBody(t, managementBaseURL+"/metrics", http.StatusOK)
		if !bytes.Contains(managementMetrics, []byte("gestaltd_operation_count_total")) {
			t.Fatalf("expected management listener to expose /metrics: %s", managementMetrics)
		}

		managementRoot := getEndpointBody(t, managementBaseURL+"/", http.StatusOK)
		if !bytes.Contains(managementRoot, []byte("Prometheus metrics")) {
			t.Fatalf("expected management listener root to land on admin ui: %s", managementRoot)
		}

		managementAdmin := getEndpointBody(t, managementBaseURL+"/admin", http.StatusOK)
		if !bytes.Contains(managementAdmin, []byte("Prometheus metrics")) {
			t.Fatalf("expected management listener to expose /admin: %s", managementAdmin)
		}
		if !bytes.Contains(managementAdmin, []byte(`class="brand" href="/admin/"`)) {
			t.Fatalf("expected management admin brand link to stay on /admin: %s", managementAdmin)
		}
		if bytes.Contains(managementAdmin, []byte(`<a href="/">Client UI</a>`)) {
			t.Fatalf("did not expect management admin to link to same-origin /: %s", managementAdmin)
		}
		if !bytes.Contains(managementAdmin, []byte(`href="https://gestalt.example.test"`)) {
			t.Fatalf("expected management admin to link to configured public base url: %s", managementAdmin)
		}
	})
	if !strings.Contains(stdout, "management listener serves /admin and /metrics without Gestalt auth") {
		t.Fatalf("expected management-listener warning in stdout\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestE2EBareGestaltdUsesDotGestaltdHomeConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	configDir := filepath.Join(homeDir, ".gestaltd")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll config dir: %v", err)
	}

	port := allocateTestPort(t)
	cfg := authDatastoreConfigYAML(t, dir, "", "sqlite", filepath.Join(configDir, "gestalt.db")) + `server:
  public:
    port: ` + fmt.Sprintf("%d", port) + `
  encryptionKey: test-key
`
	cfgPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	cmd := exec.Command(gestaltdBin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = withoutEnvVar(os.Environ(), "GESTALT_CONFIG")
	cmd.Env = append(cmd.Env,
		"HOME="+homeDir,
		"GOMODCACHE="+goEnvPath(t, "GOMODCACHE"),
		"GOCACHE="+goEnvPath(t, "GOCACHE"),
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start gestaltd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForHealth(t, baseURL, 20*time.Second)
}

func goEnvPath(t *testing.T, key string) string {
	t.Helper()

	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", key, err)
	}
	return strings.TrimSpace(string(out))
}

func TestE2EValidateNonMutating(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pluginDir := setupPluginDir(t, dir)

	cfgPath := writeE2EConfig(t, dir, pluginDir, 0)
	lockPath := filepath.Join(dir, operator.InitLockfileName)

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("expected validate to succeed without init for local source plugins: %v\n%s", err, out)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("expected no lockfile after non-mutating validate")
	}
}

func TestE2EHelmChart(t *testing.T) {
	helmPath, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not installed")
	}

	chartDir := filepath.Join("..", "..", "deploy", "helm", "gestalt")

	t.Run("default chart profile boots", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		port := allocateTestPort(t)
		dbPath := filepath.Join(dir, "gestalt.db")

		cfgPath := filepath.Join(dir, "config.yaml")
		cfg := fmt.Sprintf(`datastores:
  sqlite:
    provider:
      source:
        ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
        version: 0.0.1-alpha.1
    config:
      path: %s
datastore: sqlite
server:
  encryptionKey: test-helm-key
  public:
    port: %d
ui:
  provider: none
`, dbPath, port)
		if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}

		out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
		if err != nil {
			t.Fatalf("gestaltd validate: %v\n%s", err, out)
		}

		cmd := exec.Command(gestaltdBin, "serve", "--locked", "--config", cfgPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start serve: %v", err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		})

		baseURL := fmt.Sprintf("http://localhost:%d", port)
		waitForHealth(t, baseURL, 20*time.Second)
		waitForReady(t, baseURL, 20*time.Second)
	})

	t.Run("ingress paths render from values", func(t *testing.T) {
		t.Parallel()

		rendered := renderHelmChart(t, helmPath, chartDir,
			"--set", "ingress.enabled=true",
			"--set-string", "ingress.hosts[0].host=gestalt.example.com",
			"--set-string", "ingress.hosts[0].paths[0].path=/gestalt",
			"--set-string", "ingress.hosts[0].paths[0].pathType=Prefix",
		)

		for _, want := range []string{
			`host: "gestalt.example.com"`,
			`path: "/gestalt"`,
			`pathType: Prefix`,
		} {
			if !strings.Contains(rendered, want) {
				t.Fatalf("expected rendered manifest to contain %q\n%s", want, rendered)
			}
		}
	})

	t.Run("management service renders from values", func(t *testing.T) {
		t.Parallel()

		rendered := renderHelmChart(t, helmPath, chartDir,
			"--set", "managementService.enabled=true",
			"--set", "managementService.port=9090",
			"--set", "config.server.management.port=9090",
			"--set-string", "config.server.management.host=0.0.0.0",
		)

		for _, want := range []string{
			`kind: Service`,
			`name: test-release-gestalt-management`,
			`port: 9090`,
			`targetPort: management`,
			`containerPort: 9090`,
		} {
			if !strings.Contains(rendered, want) {
				t.Fatalf("expected rendered manifest to contain %q\n%s", want, rendered)
			}
		}
	})
}

func withoutEnvVar(env []string, name string) []string {
	prefix := name + "="
	filtered := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func renderHelmChart(t *testing.T, helmPath, chartDir string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{"template", "test-release", chartDir}, extraArgs...)
	out, err := exec.Command(helmPath, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsUnknownYAMLField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pluginYAML string
		wantError  string
	}{
		{
			name: "bogus field",
			pluginYAML: `provider:
  source:
    path: /tmp/manifest.yaml
bogus: true`,
			wantError: "bogus",
		},
		{
			name: "removed plugin connection field",
			pluginYAML: `provider:
  source:
    path: /tmp/manifest.yaml
connection: default`,
			wantError: "connection",
		},
		{
			name: "removed provider params field",
			pluginYAML: `provider:
  source:
    path: /tmp/manifest.yaml
params:
  tenant:
    required: true`,
			wantError: "params",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfg := authDatastoreConfigYAML(t, dir, "local", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  encryptionKey: test-key
plugins:
  example:
    %s
`, strings.ReplaceAll(tc.pluginYAML, "\n", "\n    "))
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
				t.Fatalf("WriteFile config: %v", err)
			}

			out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
			if err == nil {
				t.Fatalf("expected validate to fail for unknown field, output: %s", out)
			}
			if !strings.Contains(string(out), tc.wantError) || !strings.Contains(string(out), "parsing config YAML") {
				t.Fatalf("expected output to mention %q and YAML parsing, got: %s", tc.wantError, out)
			}
		})

	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EDefaultStartRejectsUnknownYAMLField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := authDatastoreConfigYAML(t, dir, "local", "sqlite", filepath.Join(dir, "gestalt.db")) + `server:
  encryptionKey: test-key
  typo: true
plugins:
  example:
    provider:
      source:
        path: /tmp/manifest.yaml
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected default start command to fail for unknown field, output: %s", out)
	}
	if !strings.Contains(string(out), "typo") || !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention unknown field and YAML parsing, got: %s", out)
	}
}

//nolint:paralleltest // Spawns the CLI binary; keeping it serial avoids package-level e2e flake.
func TestE2EValidateRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`{{{invalid yaml`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	out, err := exec.Command(gestaltdBin, "validate", "--config", cfgPath).CombinedOutput()
	if err == nil {
		t.Fatalf("expected validate to fail for malformed YAML, output: %s", out)
	}
	if !strings.Contains(string(out), "parsing config YAML") {
		t.Fatalf("expected output to mention YAML parsing failure, got: %s", out)
	}
}

func setupPluginDir(t *testing.T, baseDir string) string {
	t.Helper()
	return setupPluginDirWithVersion(t, baseDir, "0.0.1-alpha.1")
}

func setupPluginDirWithVersion(t *testing.T, baseDir, version string) string {
	t.Helper()

	pluginDir := filepath.Join(baseDir, "plugin-src")
	testutil.CopyExampleProviderPlugin(t, pluginDir)
	manifest := &pluginmanifestv1.Manifest{
		Source:      "github.com/test/plugins/provider",
		Version:     version,
		DisplayName: "Example Provider",
		Description: "A minimal example provider built with the public SDK",
		Plugin:      &pluginmanifestv1.Plugin{},
	}
	writeManifestFile(t, pluginDir, manifest)
	return pluginDir
}

func setupAuthProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "auth", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "go.mod", []byte(testutil.GeneratedProviderModuleSource(t, "example.com/providers/auth/"+name)), 0o644)
	writeTestFile(t, providerDir, "go.sum", testutil.GeneratedProviderModuleSum(t), 0o644)
	writeTestFile(t, providerDir, "auth.go", []byte(authProviderSource(name)), 0o644)
	artifactRel := filepath.ToSlash(filepath.Join("artifacts", runtime.GOOS, runtime.GOARCH, "auth-provider"))
	artifactPath := filepath.Join(providerDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(artifactPath), err)
	}
	if _, err := pluginpkg.BuildSourceComponentReleaseBinary(providerDir, artifactPath, pluginmanifestv1.KindAuth, runtime.GOOS, runtime.GOARCH, ""); err != nil {
		t.Fatalf("BuildSourceComponentReleaseBinary(%s): %v", providerDir, err)
	}
	writeManifestFile(t, providerDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/test/providers/auth/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Auth " + name,
		Auth:        &pluginmanifestv1.AuthMetadata{},
		Artifacts: []pluginmanifestv1.Artifact{
			{OS: runtime.GOOS, Arch: runtime.GOARCH, Path: artifactRel},
		},
		Entrypoints: pluginmanifestv1.Entrypoints{
			Auth: &pluginmanifestv1.Entrypoint{ArtifactPath: artifactRel},
		},
	})
	return providerDir
}

func authProviderSource(name string) string {
	source := testutil.GeneratedAuthPackageSource()
	displayName := name
	if name != "" {
		displayName = strings.ToUpper(name[:1]) + name[1:]
	}
	source = strings.Replace(source, `Name:        "generated-auth"`, fmt.Sprintf(`Name:        %q`, name), 1)
	source = strings.Replace(source, `DisplayName: "Generated Auth"`, fmt.Sprintf(`DisplayName: %q`, displayName), 1)
	return source
}

func setupWebProviderDir(t *testing.T, baseDir, name string) string {
	t.Helper()

	providerDir := filepath.Join(baseDir, "web", name)
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", providerDir, err)
	}
	writeTestFile(t, providerDir, "out/index.html", []byte("<!doctype html><title>test web ui</title>"), 0o644)
	writeManifestFile(t, providerDir, &pluginmanifestv1.Manifest{
		Source:      "github.com/test/providers/web/" + name,
		Version:     "0.0.1-alpha.1",
		DisplayName: "Test Web " + name,
		WebUI: &pluginmanifestv1.WebUIMetadata{
			AssetRoot: "out",
		},
	})
	return providerDir
}

func componentProviderManifestPath(t *testing.T, providerDir string) string {
	t.Helper()

	manifestPath, err := pluginpkg.FindManifestFile(providerDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", providerDir, err)
	}
	return manifestPath
}

func authDatastoreConfigYAML(t *testing.T, dir, authName, datastoreName, dbPath string) string {
	t.Helper()

	authBlock := ""
	if authName != "" {
		authManifestPath := componentProviderManifestPath(t, setupAuthProviderDir(t, dir, authName))
		authBlock = fmt.Sprintf(`auth:
  provider:
    source:
      path: %s
`, authManifestPath)
	}
	return fmt.Sprintf(`%sdatastores:
  %s:
    provider:
      source:
        ref: github.com/valon-technologies/gestalt-providers/datastore/sqlite
        version: 0.0.1-alpha.1
    config:
      path: %s
datastore: %s
egress:
  allowPrivateNetworks: true
ui:
  provider: none
`, authBlock, datastoreName, dbPath, datastoreName)
}

func writeManifestFile(t *testing.T, pluginDir string, manifest *pluginmanifestv1.Manifest) {
	t.Helper()
	data, err := pluginpkg.EncodeSourceManifestFormat(manifest, pluginpkg.ManifestFormatYAML)
	if err != nil {
		t.Fatalf("EncodeSourceManifestFormat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.yaml"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeE2EConfig(t *testing.T, dir, pluginDir string, port int) string {
	t.Helper()
	return writeE2EConfigWithPaths(t, dir, pluginDir, filepath.Join(dir, "gestalt.db"), "", port)
}

func writeSplitListenerE2EConfig(t *testing.T, dir, pluginDir string, publicPort, managementPort int) string {
	t.Helper()

	if publicPort == 0 {
		publicPort = 18080
	}
	if managementPort == 0 {
		managementPort = 19090
	}
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := authDatastoreConfigYAML(t, dir, "", "sqlite", filepath.Join(dir, "gestalt.db")) + fmt.Sprintf(`server:
  encryptionKey: test-e2e-key
  baseUrl: https://gestalt.example.test
  public:
    port: %d
  management:
    host: 127.0.0.1
    port: %d
plugins:
  example:
    provider:
      source:
        path: %s
`, publicPort, managementPort, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writeE2EConfigWithPaths(t *testing.T, dir, pluginDir, dbPath, artifactsDir string, port int) string {
	t.Helper()

	if port == 0 {
		port = 18080
	}
	manifestPath, err := pluginpkg.FindManifestFile(pluginDir)
	if err != nil {
		t.Fatalf("FindManifestFile(%s): %v", pluginDir, err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	serverBlock := fmt.Sprintf(`server:
  public:
    port: %d
  encryptionKey: test-e2e-key
`, port)
	if artifactsDir != "" {
		serverBlock += fmt.Sprintf("  artifactsDir: %s\n", artifactsDir)
	}
	cfg := authDatastoreConfigYAML(t, dir, "", "sqlite", dbPath) + fmt.Sprintf(`%splugins:
  example:
    provider:
      source:
        path: %s
`, serverBlock, manifestPath)

	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func serveLockedAndExerciseExample(t *testing.T, cfgPath string, port int, artifactsDir string, exercise func(t *testing.T, baseURL string)) (string, string) {
	t.Helper()

	args := []string{"serve", "--locked", "--config", cfgPath}
	if artifactsDir != "" {
		args = append(args, "--artifacts-dir", artifactsDir)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(gestaltdBin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	})

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	waitForReady(t, baseURL, 30*time.Second)

	exercise(t, baseURL)

	stopped = true
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()
	return stdout.String(), stderr.String()
}

func serveLockedAndExerciseWithManagement(t *testing.T, cfgPath string, publicPort, managementPort int, artifactsDir string, exercise func(t *testing.T, publicBaseURL, managementBaseURL string)) (string, string) {
	t.Helper()

	args := []string{"serve", "--locked", "--config", cfgPath}
	if artifactsDir != "" {
		args = append(args, "--artifacts-dir", artifactsDir)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command(gestaltdBin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Signal(os.Interrupt)
			_ = cmd.Wait()
		}
	})

	publicBaseURL := fmt.Sprintf("http://localhost:%d", publicPort)
	managementBaseURL := fmt.Sprintf("http://localhost:%d", managementPort)
	waitForReady(t, publicBaseURL, 30*time.Second)
	waitForReady(t, managementBaseURL, 30*time.Second)

	exercise(t, publicBaseURL, managementBaseURL)

	stopped = true
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()
	return stdout.String(), stderr.String()
}

func serveLockedAndInvokeExampleEcho(t *testing.T, cfgPath string, port int, artifactsDir string) (string, string) {
	t.Helper()

	return serveLockedAndExerciseExample(t, cfgPath, port, artifactsDir, func(t *testing.T, baseURL string) {
		body := invokeExampleOperation(t, baseURL, "echo", `{"message":"hello"}`, http.StatusOK)

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal: %v\nbody: %s", err, body)
		}
		if result["echo"] != "hello" {
			t.Fatalf("expected echo=hello, got %v", result)
		}

		body = invokeExampleOperation(t, baseURL, "status", `{}`, http.StatusOK)
		result = map[string]any{}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal status: %v\nbody: %s", err, body)
		}
		if result["name"] != "example" {
			t.Fatalf("expected configured name=example, got %v", result)
		}
		if result["greeting"] != "" {
			t.Fatalf("expected empty greeting without provider config, got %v", result)
		}
	})
}

func invokeExampleOperation(t *testing.T, baseURL, operation, requestBody string, wantStatus int) []byte {
	t.Helper()

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/example/"+operation, strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("invoke %q returned %d, want %d: %s", operation, resp.StatusCode, wantStatus, respBody)
	}
	return respBody
}

func getEndpointBody(t *testing.T, url string, wantStatus int) []byte {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s returned %d, want %d: %s", url, resp.StatusCode, wantStatus, body)
	}
	return body
}

func requireMetricPayload(t *testing.T, exports [][]byte, stdout, stderr string, parts ...string) {
	t.Helper()

	for _, body := range exports {
		if payloadContainsAll(body, parts...) {
			return
		}
	}

	t.Fatalf("expected OTLP metric payload to contain %q\nstdout:\n%s\nstderr:\n%s", parts, stdout, stderr)
}

func payloadContainsAll(body []byte, parts ...string) bool {
	for _, part := range parts {
		if !bytes.Contains(body, []byte(part)) {
			return false
		}
	}
	return true
}

var nextTestPort atomic.Int32 // zero value; first allocation returns 19100

func allocateTestPort(t *testing.T) int {
	t.Helper()
	return int(nextTestPort.Add(1)) + 19099
}

func waitForHealth(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	waitForEndpoint(t, baseURL+"/health", timeout)
}

func waitForReady(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	waitForEndpoint(t, baseURL+"/ready", timeout)
}

func waitForEndpoint(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s did not return 200 within %s", url, timeout)
}
