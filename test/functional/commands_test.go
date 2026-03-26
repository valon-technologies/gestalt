package functional

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGestaltdPluginLifecycleCommands(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, 0, "")

	helpOutput := runGestaltdCommand(t, "--help")
	if !strings.Contains(helpOutput, "gestaltd serve") || !strings.Contains(helpOutput, "gestaltd plugin <command> [flags]") {
		t.Fatalf("unexpected help output:\n%s", helpOutput)
	}

	src := buildPluginFixture(t, dir)
	packagePath := filepath.Join(dir, "acme-provider-0.1.0.tar.gz")

	packageOutput := runGestaltdCommand(t, "plugin", "package", "--input", src, "--output", packagePath)
	if !strings.Contains(packageOutput, "packaged") {
		t.Fatalf("unexpected package output:\n%s", packageOutput)
	}
	if _, err := os.Stat(packagePath); err != nil {
		t.Fatalf("expected package archive at %s: %v", packagePath, err)
	}

	inspectOutput := runGestaltdCommand(t, "plugin", "inspect", packagePath)
	if !strings.Contains(inspectOutput, "id: acme/provider") {
		t.Fatalf("unexpected inspect output:\n%s", inspectOutput)
	}

	installOutput := runGestaltdCommand(t, "plugin", "install", "--config", cfgPath, packagePath)
	if !strings.Contains(installOutput, "installed acme/provider@0.1.0") {
		t.Fatalf("unexpected install output:\n%s", installOutput)
	}

	installedExecutable := filepath.Join(dir, ".gestalt", "plugins", "acme", "provider", "0.1.0", "artifacts", runtime.GOOS, runtime.GOARCH, "provider")
	if _, err := os.Stat(installedExecutable); err != nil {
		t.Fatalf("expected installed executable at %s: %v", installedExecutable, err)
	}

	listOutput := runGestaltdCommand(t, "plugin", "list", "--config", cfgPath)
	if !strings.Contains(listOutput, "acme/provider@0.1.0") {
		t.Fatalf("unexpected list output:\n%s", listOutput)
	}

	validateOutput := runGestaltdCommand(t, "validate", "--config", cfgPath)
	if !strings.Contains(validateOutput, "config ok") {
		t.Fatalf("unexpected validate output:\n%s", validateOutput)
	}
}

func TestGestaltdPrepareAndServePreparedRESTProvider(t *testing.T) {
	var backend *httptest.Server
	backend = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openapi.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"openapi": "3.0.0",
				"info":    map[string]any{"title": "Functional REST API"},
				"servers": []map[string]any{{"url": backend.URL}},
				"paths": map[string]any{
					"/items": map[string]any{
						"get": map[string]any{
							"operationId": "list_items",
							"summary":     "List items",
						},
					},
				},
			})
		case "/items":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []string{"alpha", "beta"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	dir := t.TempDir()
	port := freePort(t)
	cfgPath := writeConfig(t, dir, port, fmt.Sprintf(`  restapi:
    display_name: "REST API"
    connection_mode: "none"
    auth_style: "none"
    upstreams:
      - type: "rest"
        url: %q
`, backend.URL+"/openapi.json"))

	runGestaltdCommand(t, "prepare", "--config", cfgPath)

	lockPath := filepath.Join(dir, "gestalt.lock.json")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lockfile at %s: %v", lockPath, err)
	}
	providerPath := filepath.Join(dir, ".gestalt", "providers", "restapi.json")
	if _, err := os.Stat(providerPath); err != nil {
		t.Fatalf("expected prepared provider at %s: %v", providerPath, err)
	}

	validateOutput := runGestaltdCommand(t, "validate", "--config", cfgPath)
	if !strings.Contains(validateOutput, "config ok") {
		t.Fatalf("unexpected validate output:\n%s", validateOutput)
	}

	proc := startGestaltdProcess(t, cfgPath, port)
	devLogin(t, proc.Client, proc.BaseURL, "prepared@gestalt.dev")

	status, body := doJSON(t, proc.Client, http.MethodGet, proc.BaseURL+"/api/v1/integrations", nil)
	if status != http.StatusOK {
		t.Fatalf("list integrations status=%d body=%s", status, string(body))
	}
	integrations := decodeJSON[[]struct {
		Name string `json:"name"`
	}](t, body)

	foundREST := false
	for _, integration := range integrations {
		if integration.Name == "restapi" {
			foundREST = true
		}
	}
	if !foundREST {
		t.Fatalf("restapi integration missing from %+v", integrations)
	}

	status, body = doJSON(t, proc.Client, http.MethodGet, proc.BaseURL+"/api/v1/integrations/restapi/operations", nil)
	if status != http.StatusOK {
		t.Fatalf("list operations status=%d body=%s", status, string(body))
	}
	operations := decodeJSON[[]struct {
		Name string
	}](t, body)
	if len(operations) != 1 || operations[0].Name != "list_items" {
		t.Fatalf("unexpected operations: %+v", operations)
	}

	status, body = doJSON(t, proc.Client, http.MethodGet, proc.BaseURL+"/api/v1/restapi/list_items", nil)
	if status != http.StatusOK {
		t.Fatalf("invoke restapi status=%d body=%s", status, string(body))
	}
	response := decodeJSON[map[string][]string](t, body)
	if strings.Join(response["items"], ",") != "alpha,beta" {
		t.Fatalf("unexpected REST response: %+v", response)
	}
}
