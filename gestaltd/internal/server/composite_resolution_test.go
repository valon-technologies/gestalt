package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func TestExecuteOperation_CompositeStaticRESTBypassesMCPSessionResolution(t *testing.T) {
	t.Parallel()

	var (
		gotToken        string
		mcpCatalogCalls atomic.Int32
	)

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "notion",
			ConnMode: core.ConnectionModeSubject,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				gotToken = token
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token))}, nil
			},
		},
		catalog: serverTestCatalog("notion", []catalog.CatalogOperation{
			{ID: "api_get_self", Description: "Retrieve self", Method: http.MethodGet, Transport: catalog.TransportREST},
		}),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{
				N:        "notion",
				ConnMode: core.ConnectionModeSubject,
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			mcpCatalogCalls.Add(1)
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-oauth",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:OAuth",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/notion/api_get_self", nil)
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

func TestExecuteOperation_CompositeSearchUsesOAuthWithoutMCPCatalogDiscovery(t *testing.T) {
	t.Parallel()

	var (
		gotToken        string
		mcpCatalogCalls atomic.Int32
	)

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{
			N:        "notion",
			ConnMode: core.ConnectionModeSubject,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				gotToken = token
				return &core.OperationResult{Status: http.StatusOK, Body: []byte(fmt.Sprintf(`{"operation":%q}`, op))}, nil
			},
		},
		catalog: serverTestCatalog("notion", []catalog.CatalogOperation{
			{ID: "search", Description: "Search", Method: http.MethodPost, Transport: catalog.TransportREST},
		}),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			mcpCatalogCalls.Add(1)
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-oauth",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:OAuth",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/notion/search", strings.NewReader("{}"))
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

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		catalog:         serverTestCatalog("notion", nil),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
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
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-oauth",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:OAuth",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
	})
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-mcp",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:MCP",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "mcp-token"},
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/notion/get_page_content", strings.NewReader("{}"))
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

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		catalog:         serverTestCatalog("notion", nil),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
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
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-mcp",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:MCP",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "mcp-token"},
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/notion/get_page_content", nil)
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

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		catalog:         serverTestCatalog("notion", nil),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: func(context.Context, string) (*catalog.Catalog, error) {
			t.Fatal("MCP catalog discovery should not run without MCP credential")
			return nil, nil
		},
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-oauth",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:OAuth",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "oauth-token"},
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/notion/get_page_content", strings.NewReader("{}"))
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

	apiProv := &stubIntegrationWithCatalog{
		StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		catalog:         serverTestCatalog("notion", nil),
	}
	mcpUpstream := &stubIntegrationWithSessionCatalog{
		stubIntegrationWithOps: stubIntegrationWithOps{
			StubIntegration: coretesting.StubIntegration{N: "notion", ConnMode: core.ConnectionModeSubject},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := testutil.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok-mcp",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "notion:MCP",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "stale-mcp-token"},
	})

	broker := invocation.NewBroker(
		providers,
		svc.Users,
		svc.ExternalCredentials,
		invocation.WithConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "OAuth"})),
		invocation.WithMCPConnectionMapper(invocation.ConnectionMap(map[string]string{"notion": "MCP"})),
	)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.Services = svc
		cfg.Invoker = broker
		cfg.CatalogConnection = map[string]string{"notion": "OAuth"}
		cfg.MCPConnection = map[string]string{"notion": "MCP"}
	})
	testutil.CloseOnCleanup(t, ts)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/notion/get_page_content", strings.NewReader("{}"))
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
