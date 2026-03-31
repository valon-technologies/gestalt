package pluginhost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func testManifest(baseURL string) *pluginmanifestv1.Manifest {
	return &pluginmanifestv1.Manifest{
		Source:      "github.com/acme/plugins/testapi",
		Version:     "1.0.0",
		DisplayName: "Test API",
		Description: "A test API provider",
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL: baseURL,
			Auth:    &pluginmanifestv1.ProviderAuth{Type: pluginmanifestv1.AuthTypeBearer},
			Operations: []pluginmanifestv1.ProviderOperation{
				{
					Name:        "list_items",
					Description: "List all items",
					Method:      "GET",
					Path:        "/items",
					Parameters: []pluginmanifestv1.ProviderParameter{
						{Name: "limit", Type: "int", In: "query"},
						{Name: "cursor", Type: "string", In: "query"},
					},
				},
				{
					Name:        "create_item",
					Description: "Create a new item",
					Method:      "POST",
					Path:        "/items",
					Parameters: []pluginmanifestv1.ProviderParameter{
						{Name: "name", Type: "string", In: "body", Required: true},
						{Name: "value", Type: "string", In: "body"},
					},
				},
				{
					Name:        "get_item",
					Description: "Get item by ID",
					Method:      "GET",
					Path:        "/items/{id}",
					Parameters: []pluginmanifestv1.ProviderParameter{
						{Name: "id", Type: "string", In: "path", Required: true},
					},
				},
				{
					Name:        "update_item",
					Description: "Update an item with query filter",
					Method:      "POST",
					Path:        "/items/{id}",
					Parameters: []pluginmanifestv1.ProviderParameter{
						{Name: "id", Type: "string", In: "path", Required: true},
						{Name: "name", Type: "string", In: "body"},
						{Name: "dry_run", Type: "bool", In: "query"},
					},
				},
			},
		},
	}
}

type capturedRequest struct {
	method      string
	path        string
	query       map[string]string
	body        map[string]any
	authHeader  string
	contentType string
}

func captureHandler(t *testing.T, captured *capturedRequest, respond map[string]any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.authHeader = r.Header.Get("Authorization")
		captured.contentType = r.Header.Get("Content-Type")
		captured.query = make(map[string]string)
		for k, v := range r.URL.Query() {
			captured.query[k] = v[0]
		}
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			if len(data) > 0 {
				captured.body = make(map[string]any)
				_ = json.Unmarshal(data, &captured.body)
			}
		}
		_ = json.NewEncoder(w).Encode(respond)
	}
}

func TestDeclarativeProvider_GETWithQueryParams(t *testing.T) {
	t.Parallel()

	var req capturedRequest
	srv := httptest.NewServer(captureHandler(t, &req, map[string]any{"ok": true}))
	defer srv.Close()

	p, err := NewDeclarativeProvider(testManifest(srv.URL), srv.Client())
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	result, err := p.Execute(context.Background(), "list_items", map[string]any{
		"limit":  float64(10),
		"cursor": "abc123",
	}, "test-token-000")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if req.method != "GET" {
		t.Errorf("method = %s, want GET", req.method)
	}
	if req.path != "/items" {
		t.Errorf("path = %s, want /items", req.path)
	}
	if req.query["limit"] != "10" {
		t.Errorf("query limit = %s, want 10", req.query["limit"])
	}
	if req.query["cursor"] != "abc123" {
		t.Errorf("query cursor = %s, want abc123", req.query["cursor"])
	}
	if req.authHeader != "Bearer test-token-000" {
		t.Errorf("auth = %s, want Bearer test-token-000", req.authHeader)
	}
	if result.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestDeclarativeProvider_POSTWithBodyParams(t *testing.T) {
	t.Parallel()

	var req capturedRequest
	srv := httptest.NewServer(captureHandler(t, &req, map[string]any{"ok": true, "id": "new-1"}))
	defer srv.Close()

	p, err := NewDeclarativeProvider(testManifest(srv.URL), srv.Client())
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	result, err := p.Execute(context.Background(), "create_item", map[string]any{
		"name":  "widget",
		"value": "42",
	}, "test-token-000")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if req.method != "POST" {
		t.Errorf("method = %s, want POST", req.method)
	}
	if req.path != "/items" {
		t.Errorf("path = %s, want /items", req.path)
	}
	if req.body["name"] != "widget" {
		t.Errorf("body name = %v, want widget", req.body["name"])
	}
	if req.body["value"] != "42" {
		t.Errorf("body value = %v, want 42", req.body["value"])
	}
	if req.authHeader != "Bearer test-token-000" {
		t.Errorf("auth = %s, want Bearer test-token-000", req.authHeader)
	}
	if result.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", result.Status)
	}
}

func TestDeclarativeProvider_PathParamSubstitution(t *testing.T) {
	t.Parallel()

	var req capturedRequest
	srv := httptest.NewServer(captureHandler(t, &req, map[string]any{"id": "item-42", "name": "thing"}))
	defer srv.Close()

	p, err := NewDeclarativeProvider(testManifest(srv.URL), srv.Client())
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	_, err = p.Execute(context.Background(), "get_item", map[string]any{
		"id": "item-42",
	}, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if req.method != "GET" {
		t.Errorf("method = %s, want GET", req.method)
	}
	if req.path != "/items/item-42" {
		t.Errorf("path = %s, want /items/item-42", req.path)
	}
}

func TestDeclarativeProvider_MixedQueryAndBodyParams(t *testing.T) {
	t.Parallel()

	var req capturedRequest
	srv := httptest.NewServer(captureHandler(t, &req, map[string]any{"ok": true}))
	defer srv.Close()

	p, err := NewDeclarativeProvider(testManifest(srv.URL), srv.Client())
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	_, err = p.Execute(context.Background(), "update_item", map[string]any{
		"id":      "item-7",
		"name":    "updated",
		"dry_run": true,
	}, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if req.method != "POST" {
		t.Errorf("method = %s, want POST", req.method)
	}
	if req.path != "/items/item-7" {
		t.Errorf("path = %s, want /items/item-7", req.path)
	}
	if req.query["dry_run"] != "true" {
		t.Errorf("query dry_run = %s, want true", req.query["dry_run"])
	}
	if req.body["name"] != "updated" {
		t.Errorf("body name = %v, want updated", req.body["name"])
	}
	if _, has := req.body["id"]; has {
		t.Error("body should not contain path param 'id'")
	}
	if _, has := req.body["dry_run"]; has {
		t.Error("body should not contain query param 'dry_run'")
	}
}

func TestDeclarativeProvider_UnknownOperation(t *testing.T) {
	t.Parallel()

	p, err := NewDeclarativeProvider(testManifest("http://localhost:0"), nil)
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	result, err := p.Execute(context.Background(), "nonexistent", nil, "tok")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", result.Status)
	}
}

func TestDeclarativeProvider_Metadata(t *testing.T) {
	t.Parallel()

	p, err := NewDeclarativeProvider(testManifest("http://example.com"), nil)
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	if p.Name() != "github.com/acme/plugins/testapi" {
		t.Errorf("Name = %s, want github.com/acme/plugins/testapi", p.Name())
	}
	if p.DisplayName() != "Test API" {
		t.Errorf("DisplayName = %s, want Test API", p.DisplayName())
	}
	if p.Description() != "A test API provider" {
		t.Errorf("Description = %s, want A test API provider", p.Description())
	}
	if p.ConnectionMode() != "user" {
		t.Errorf("ConnectionMode = %s, want user", p.ConnectionMode())
	}

	ops := p.ListOperations()
	if len(ops) != 4 {
		t.Fatalf("ListOperations returned %d ops, want 4", len(ops))
	}
	if ops[0].Name != "list_items" {
		t.Errorf("ops[0].Name = %s, want list_items", ops[0].Name)
	}
}

func TestDeclarativeProvider_AuthTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		authType string
		want     []string
	}{
		{"oauth2", pluginmanifestv1.AuthTypeOAuth2, []string{"oauth2"}},
		{"bearer", pluginmanifestv1.AuthTypeBearer, []string{"manual"}},
		{"manual", pluginmanifestv1.AuthTypeManual, []string{"manual"}},
		{"none", pluginmanifestv1.AuthTypeNone, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := testManifest("http://example.com")
			m.Provider.Auth = &pluginmanifestv1.ProviderAuth{Type: tc.authType}
			if tc.authType == pluginmanifestv1.AuthTypeOAuth2 {
				m.Provider.Auth.AuthorizationURL = "https://example.com/auth"
				m.Provider.Auth.TokenURL = "https://example.com/token"
			}
			p, err := NewDeclarativeProvider(m, nil)
			if err != nil {
				t.Fatalf("NewDeclarativeProvider: %v", err)
			}
			got := p.AuthTypes()
			if len(got) != len(tc.want) {
				t.Fatalf("AuthTypes = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("AuthTypes[%d] = %s, want %s", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDeclarativeProviderIcon(t *testing.T) {
	t.Parallel()

	p, err := NewDeclarativeProvider(testManifest("http://example.com"), nil)
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	var _ core.CatalogProvider = p

	if cat := p.Catalog(); cat != nil {
		t.Fatalf("expected nil catalog before SetIconSVG, got %+v", cat)
	}

	const testSVG = `<svg xmlns="http://www.w3.org/2000/svg"><circle r="10"/></svg>`
	p.SetIconSVG(testSVG)

	cat := p.Catalog()
	if cat == nil {
		t.Fatal("expected non-nil catalog after SetIconSVG")
	}
	if cat.IconSVG != testSVG {
		t.Fatalf("IconSVG = %q, want %q", cat.IconSVG, testSVG)
	}
}

func TestDeclarativeProvider_OAuthAuthorizationURL(t *testing.T) {
	t.Parallel()

	m := testManifest("http://example.com")
	m.Provider.Auth = &pluginmanifestv1.ProviderAuth{
		Type:             pluginmanifestv1.AuthTypeOAuth2,
		AuthorizationURL: "https://example.com/oauth/authorize",
		TokenURL:         "https://example.com/oauth/token",
		Scopes:           []string{"read", "write"},
	}

	p, err := NewDeclarativeProvider(m, nil)
	if err != nil {
		t.Fatalf("NewDeclarativeProvider: %v", err)
	}

	url := p.AuthorizationURL("state123", []string{"read"})
	if url != "https://example.com/oauth/authorize" {
		t.Errorf("AuthorizationURL = %s, want https://example.com/oauth/authorize", url)
	}
}
