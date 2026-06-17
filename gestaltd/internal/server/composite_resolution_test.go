package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/composite"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type compositeNotionEnv struct {
	ts *httptest.Server
}

type compositeNotionConfig struct {
	apiOperations []catalog.CatalogOperation
	apiExecuteFn  func(context.Context, string, map[string]any, string) (*core.OperationResult, error)
	mcpCatalogFn  func(context.Context, string) (*catalog.Catalog, error)
	seedOAuth     bool
	seedMCP       bool
	mcpToken      string
}

func setupCompositeNotion(t *testing.T, cfg compositeNotionConfig) compositeNotionEnv {
	t.Helper()

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:         "notion",
			ConnMode:  core.ConnectionModeSubject,
			ExecuteFn: cfg.apiExecuteFn,
		},
		catalog: serverTestCatalog("notion", cfg.apiOperations),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: cfg.mcpCatalogFn,
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	if cfg.seedOAuth {
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-oauth",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "notion:OAuth",
			Qualifier: "default",
			Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
		})
	}
	if cfg.seedMCP {
		mcpToken := cfg.mcpToken
		if mcpToken == "" {
			mcpToken = "mcp-token"
		}
		seedToken(t, svc, &core.ExternalCredential{
			ID:        "tok-mcp",
			Subject:   principal.UserSubjectID(u.ID),
			Audience:  "notion:MCP",
			Qualifier: "default",
			Grant:     &core.ExternalCredentialGrant{AccessToken: mcpToken},
		})
	}

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(serverCfg *server.Config) {
		serverCfg.Providers = providers
		serverCfg.Services = svc
		serverCfg.Invoker = broker
		serverCfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		serverCfg.MCPConnection = map[string]string{"notion": "MCP"}
	})
	testutil.CloseOnCleanup(t, ts)

	return compositeNotionEnv{ts: ts}
}

func TestExecuteOperation_CompositeSearchUsesOAuthWithoutMCPCatalogDiscovery(t *testing.T) {
	t.Parallel()

	var (
		gotToken        string
		mcpCatalogCalls atomic.Int32
	)

	env := setupCompositeNotion(t, compositeNotionConfig{
		apiOperations: []catalog.CatalogOperation{
			{ID: "search", Description: "Search", Method: http.MethodPost, Transport: catalog.TransportREST},
		},
		apiExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
			gotToken = token
			return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q}`, op))}, nil
		},
		mcpCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			mcpCatalogCalls.Add(1)
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
		seedOAuth: true,
	})

	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/api/v1/notion/search", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if gotToken != "oauth-token" {
		t.Fatalf("execute token = %q, want %q", gotToken, "oauth-token")
	}
	if got := mcpCatalogCalls.Load(); got != 0 {
		t.Fatalf("catalog calls = %d, want 0", got)
	}
}

func TestExecuteOperation_CompositeMCPOperationUsesMCPToken(t *testing.T) {
	t.Parallel()

	var mcpCatalogCalls atomic.Int32

	env := setupCompositeNotion(t, compositeNotionConfig{
		seedOAuth: true,
		seedMCP:   true,
		mcpCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			mcpCatalogCalls.Add(1)
			if token != "mcp-token" {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			return &catalog.Catalog{
				Name: "notion",
				Operations: []catalog.CatalogOperation{
					{ID: "get_page_content", Description: "Get page content", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
				},
			}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/api/v1/notion/get_page_content", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if got := mcpCatalogCalls.Load(); got < 1 {
		t.Fatalf("catalog calls = %d, want at least 1", got)
	}
}

func TestExecuteOperation_CompositeMCPOperationRejectsGETWithAllowPost(t *testing.T) {
	t.Parallel()

	env := setupCompositeNotion(t, compositeNotionConfig{
		seedMCP: true,
		mcpCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			if token != "mcp-token" {
				return nil, fmt.Errorf("unexpected token %q", token)
			}
			return &catalog.Catalog{
				Name: "notion",
				Operations: []catalog.CatalogOperation{
					{ID: "get_page_content", Description: "Get page content", Method: http.MethodPost, Transport: catalog.TransportMCPPassthrough},
				},
			}, nil
		},
	})

	req, _ := http.NewRequest(http.MethodGet, env.ts.URL+"/api/v1/notion/get_page_content", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 405, got %d: %s", resp.StatusCode, body)
	}
	if allow := resp.Header.Get("Allow"); allow != http.MethodPost {
		t.Fatalf("Allow header = %q, want %q", allow, http.MethodPost)
	}
}

func TestExecuteOperation_CompositeMCPOperationWithoutCredentialReturnsNotConnected(t *testing.T) {
	t.Parallel()

	env := setupCompositeNotion(t, compositeNotionConfig{
		seedOAuth: true,
		mcpCatalogFn: func(context.Context, string) (*catalog.Catalog, error) {
			t.Fatal("MCP catalog discovery should not run without MCP credential")
			return nil, nil
		},
	})

	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/api/v1/notion/get_page_content", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412, got %d: %s", resp.StatusCode, body)
	}
}

func TestExecuteOperation_CompositeMCPAuthFailureReturnsReconnectRequired(t *testing.T) {
	t.Parallel()

	env := setupCompositeNotion(t, compositeNotionConfig{
		seedMCP:  true,
		mcpToken: "stale-mcp-token",
		mcpCatalogFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
	})

	req, _ := http.NewRequest(http.MethodPost, env.ts.URL+"/api/v1/notion/get_page_content", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412, got %d: %s", resp.StatusCode, body)
	}

	var errResp struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != "reconnect_required" {
		t.Fatalf("error code = %q, want reconnect_required", errResp.Code)
	}
}
