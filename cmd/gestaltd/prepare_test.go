package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareConfigWritesLockfileAndHiddenProviders(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	defer openAPIServer.Close()

	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	if err := prepareConfig(cfgPath); err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}

	lockPath := filepath.Join(dir, preparedLockfileName)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stat lockfile: %v", err)
	}
	providerPath := filepath.Join(dir, filepath.FromSlash(preparedProvidersDir), "restapi.json")
	if _, err := os.Stat(providerPath); err != nil {
		t.Fatalf("stat provider artifact: %v", err)
	}

	lock, err := readPreparedLockfile(lockPath)
	if err != nil {
		t.Fatalf("readPreparedLockfile: %v", err)
	}
	entry, ok := lock.Providers["restapi"]
	if !ok {
		t.Fatalf("lockfile missing restapi entry: %+v", lock.Providers)
	}
	if entry.Provider != ".gestalt/providers/restapi.json" {
		t.Fatalf("entry.Provider = %q, want %q", entry.Provider, ".gestalt/providers/restapi.json")
	}
	if entry.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
}

func TestLoadConfigForExecutionAutoPrepareThenServeOffline(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	_, _, preparedProviders, err := loadConfigForExecution(cfgPath, providerResolutionAuto)
	if err != nil {
		t.Fatalf("loadConfigForExecution auto: %v", err)
	}
	gotProvider := preparedProviders["restapi"]
	if gotProvider == "" {
		t.Fatal("expected prepared provider path to be injected")
	}

	openAPIServer.Close()

	_, _, preparedProviders, err = loadConfigForExecution(cfgPath, providerResolutionRequire)
	if err != nil {
		t.Fatalf("loadConfigForExecution require: %v", err)
	}
	if preparedProviders["restapi"] == "" {
		t.Fatal("expected prepared provider path in strict serve mode")
	}
}

func TestLoadConfigForExecutionRequirePreparedRejectsUnpreparedRemote(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	defer openAPIServer.Close()

	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	_, _, _, err := loadConfigForExecution(cfgPath, providerResolutionRequire)
	if err == nil {
		t.Fatal("expected strict serve to reject unprepared remote upstream")
	}
	if !strings.Contains(err.Error(), "gestaltd prepare") {
		t.Fatalf("expected prepare guidance, got: %v", err)
	}
}

func TestValidatePrefersPreparedProviders(t *testing.T) {
	t.Parallel()

	openAPIServer := newPreparedTestOpenAPIServer()
	dir := t.TempDir()
	cfgPath := writePreparedTestConfig(t, dir, openAPIServer.URL)

	if err := prepareConfig(cfgPath); err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}

	openAPIServer.Close()

	if err := validateConfig(cfgPath); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
}

func TestValidateRejectsTopLevelConfigForRuntimePlugins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  dev_mode: true
  encryption_key: test-key
runtimes:
  worker:
    providers: []
    plugin:
      command: /tmp/runtime-plugin
    config:
      poll_interval: 30s
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	err := validateConfig(cfgPath)
	if err == nil {
		t.Fatal("expected validateConfig to reject top-level config for runtime plugins")
	}
	if !strings.Contains(err.Error(), "plugin.config") {
		t.Fatalf("expected plugin.config guidance, got: %v", err)
	}
}

func TestPrepareConfigGraphQLUpstream(t *testing.T) {
	t.Parallel()

	graphQLServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"__schema": map[string]any{
					"queryType":    map[string]any{"name": "Query"},
					"mutationType": nil,
					"types": []map[string]any{
						{
							"kind": "OBJECT", "name": "Query", "description": "",
							"fields": []map[string]any{
								{
									"name": "search", "description": "Search",
									"args": []map[string]any{},
									"type": map[string]any{"kind": "OBJECT", "name": "Result", "ofType": nil},
								},
							},
							"inputFields": nil, "enumValues": nil,
						},
						{"kind": "OBJECT", "name": "Result", "description": "", "fields": []map[string]any{}, "inputFields": nil, "enumValues": nil},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer graphQLServer.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  dev_mode: true
  encryption_key: test-key
integrations:
  graphapi:
    upstreams:
      - type: graphql
        url: ` + graphQLServer.URL + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	if err := prepareConfig(cfgPath); err != nil {
		t.Fatalf("prepareConfig: %v", err)
	}

	lock, err := readPreparedLockfile(filepath.Join(dir, preparedLockfileName))
	if err != nil {
		t.Fatalf("readPreparedLockfile: %v", err)
	}
	entry, ok := lock.Providers["graphapi"]
	if !ok {
		t.Fatalf("lockfile missing graphapi entry: %+v", lock.Providers)
	}
	if entry.Provider != ".gestalt/providers/graphapi.json" {
		t.Fatalf("entry.Provider = %q, want %q", entry.Provider, ".gestalt/providers/graphapi.json")
	}

	providerPath := filepath.Join(dir, filepath.FromSlash(preparedProvidersDir), "graphapi.json")
	if _, err := os.Stat(providerPath); err != nil {
		t.Fatalf("stat graphql provider artifact: %v", err)
	}
}

func newPreparedTestOpenAPIServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"openapi": "3.0.0",
			"info":    map[string]any{"title": "REST API"},
			"servers": []map[string]any{{"url": "https://api.example.com"}},
			"paths": map[string]any{
				"/items": map[string]any{
					"get": map[string]any{
						"operationId": "listItems",
						"summary":     "List items",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func writePreparedTestConfig(t *testing.T, dir, upstreamURL string) string {
	t.Helper()

	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := `auth:
  provider: google
  config:
    client_id: test-client
    client_secret: test-secret
    redirect_url: http://localhost:8080/api/v1/auth/login/callback
datastore:
  provider: sqlite
  config:
    path: ` + filepath.Join(dir, "gestalt.db") + `
server:
  dev_mode: true
  encryption_key: test-key
integrations:
  restapi:
    display_name: REST API
    upstreams:
      - type: rest
        url: ` + upstreamURL + `
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	return cfgPath
}
