package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/mcpupstream"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
	"github.com/valon-technologies/gestalt/server/services/invocation"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func newAppCheckMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := mcpserver.NewMCPServer("check-remote", "1.0.0")
	noop := func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText("ok"), nil
	}
	srv.AddTool(mcpgo.NewTool("list_issues"), noop)
	srv.AddTool(mcpgo.NewTool("create_issue"), noop)
	ts := httptest.NewServer(mcpserver.NewStreamableHTTPServer(srv, mcpserver.WithStateLess(true)))
	t.Cleanup(ts.Close)
	return ts
}

func TestAppAdminCheckReportsAllowedOperationsAgainstLiveMCPTools(t *testing.T) {
	t.Parallel()

	const app = conformanceMountedApp
	mcpTS := newAppCheckMCPServer(t)

	tests := []struct {
		name        string
		allowed     map[string]*operationexposure.OperationOverride
		wantOK      bool
		wantUnknown []string
		wantHidden  []string
	}{
		{
			name: "typo",
			allowed: map[string]*operationexposure.OperationOverride{
				"list_issues":  {Alias: "issues.list"},
				"list_isssues": nil,
			},
			wantUnknown: []string{"list_isssues"},
			wantHidden:  []string{"create_issue"},
		},
		{
			name: "valid",
			allowed: map[string]*operationexposure.OperationOverride{
				"list_issues": {Alias: "issues.list"},
			},
			wantOK:      true,
			wantUnknown: []string{},
			wantHidden:  []string{"create_issue"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			services := testutil.NewStubServices(t)
			authorization := newConformanceAuthorization(t, conformanceSpec{
				ResourceTypes: conformanceModel("viewer"),
				Relationships: []*proto.Relationship{
					conformanceDirectGrant(conformanceSubject(conformanceAdminUserID), "admin", "app", app),
				},
			})
			upstream, err := mcpupstream.New(context.Background(), app, mcpTS.URL, core.ConnectionModeNone, nil, nil)
			if err != nil {
				t.Fatalf("mcpupstream.New: %v", err)
			}
			if err := upstream.FilterOperations(tc.allowed); err != nil {
				t.Fatalf("FilterOperations: %v", err)
			}
			providers := testutil.NewProviderRegistry(t, upstream)
			broker := invocation.NewBroker(providers, services.Users, services.ExternalCredentials,
				invocation.WithAuthorizationProvider(authorization),
			)
			ts := newTestServer(t, func(cfg *server.Config) {
				cfg.Services = services
				cfg.Auth = conformanceAuthStub()
				cfg.Authorization = authorization
				cfg.Providers = providers
				cfg.Invoker = broker
				cfg.OperationAccessChecker = broker
				cfg.AppDefs = map[string]*config.ProviderEntry{app: {}}
			})
			t.Cleanup(ts.Close)

			get := func(token string) (int, []byte) {
				t.Helper()
				req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/"+app+"/admin/check", nil)
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = resp.Body.Close() }()
				body, _ := io.ReadAll(resp.Body)
				return resp.StatusCode, body
			}

			if status, body := get(conformanceEmployeeToken); status != http.StatusForbidden {
				t.Fatalf("non-admin check status = %d, want 403: %s", status, body)
			}

			status, body := get(conformanceAdminToken)
			if status != http.StatusOK {
				t.Fatalf("admin check status = %d, want 200: %s", status, body)
			}
			var response struct {
				App      string
				OK       bool
				Surfaces []mcpupstream.AllowedOperationsCheck
			}
			if err := json.Unmarshal(body, &response); err != nil {
				t.Fatalf("decode %s: %v", body, err)
			}
			if response.App != app || response.OK != tc.wantOK || len(response.Surfaces) != 1 {
				t.Fatalf("response = %s", body)
			}
			surface := response.Surfaces[0]
			if !slices.Equal(surface.UnknownAllowedOperations, tc.wantUnknown) {
				t.Errorf("unknownAllowedOperations = %v, want %v", surface.UnknownAllowedOperations, tc.wantUnknown)
			}
			if !slices.Equal(surface.UnexposedTools, tc.wantHidden) {
				t.Errorf("unexposedTools = %v, want %v", surface.UnexposedTools, tc.wantHidden)
			}
			if !strings.Contains(string(body), `"exposedAs":"issues.list"`) {
				t.Errorf("response missing alias mapping: %s", body)
			}
		})
	}
}
