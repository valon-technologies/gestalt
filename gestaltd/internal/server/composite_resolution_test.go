package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
	"github.com/valon-technologies/gestalt/server/services/plugins/composite"
	"github.com/valon-technologies/gestalt/server/services/testutil"
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
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, op string, _ map[string]any, token string) (*core.OperationResult, error) {
				gotToken = token
				return &core.OperationResult{Status: http.StatusOK, Body: fmt.Sprintf(`{"operation":%q,"token":%q}`, op, token)}, nil
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
				ConnMode: core.ConnectionModeUser,
			},
		},
		catalogForRequestFn: func(_ context.Context, token string) (*catalog.Catalog, error) {
			mcpCatalogCalls.Add(1)
			return nil, fmt.Errorf("mcpupstream notion: initialize: transport error: unauthorized (401) for %q", token)
		},
	}

	providers := testutil.NewProviderRegistry(t, composite.New("notion", apiProv, mcpUpstream))
	svc := coretesting.NewStubServices(t)
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID: "tok-oauth", SubjectID: principal.UserSubjectID(u.ID), Integration: "notion",
		Connection: "OAuth", Instance: "default", AccessToken: "oauth-token",
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
		t.Fatalf("mcp catalog calls = %d, want 0", got)
	}
}
