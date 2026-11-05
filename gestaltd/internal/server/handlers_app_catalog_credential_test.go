package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// An app whose operations are listed through the caller's credential (an MCP
// upstream, for example) cannot list them until the caller connects. The
// access endpoints must say so with the typed 412 codes operation calls use,
// not the 503 they return for real outages, so clients can offer connect.
func TestAppCatalogCredentialFailuresReturnTypedPreconditionErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		tokenErr   error
		sessionErr error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "no credential",
			tokenErr:   fmt.Errorf("resolve token: %w", invocation.ErrNoCredential),
			wantStatus: http.StatusPreconditionFailed,
			wantCode:   "not_connected",
		},
		{
			// The upstream rejection berkadia.valon.tools logged for vci-brain.
			name:       "upstream rejects credential",
			sessionErr: errors.New("mcpupstream slack: initialize: transport error: authorization required"),
			wantStatus: http.StatusPreconditionFailed,
			wantCode:   "reconnect_required",
		},
		{
			name:       "unrelated failure",
			sessionErr: errors.New("mcpupstream slack: initialize: dial tcp: connection refused"),
			wantStatus: http.StatusServiceUnavailable,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, p := newCatalogCredentialTestServer(t, tc.tokenErr, tc.sessionErr)

			access := serveAppAccessTestRequest(t, server, http.MethodGet, nil, p)
			assertCatalogCredentialResponse(t, "access", access, tc.wantStatus, tc.wantCode)

			admin := serveAppAdminAllowedOperationsTestRequest(server, p)
			assertCatalogCredentialResponse(t, "admin allowed-operations", admin, tc.wantStatus, tc.wantCode)
		})
	}
}

func newCatalogCredentialTestServer(t *testing.T, tokenErr, sessionErr error) (*Server, *principal.Principal) {
	t.Helper()
	services := testutil.NewStubServices(t)
	// No static catalog: listing depends entirely on the caller's session.
	provider := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{name: "slack", connMode: core.ConnectionModeSubject},
		},
		sessionErr: sessionErr,
	}
	server := &Server{
		providers:            testutil.NewProviderRegistry(t, provider),
		users:                services.Users,
		appAccessProfiles:    services.AppAccessProfiles,
		appAllowedOperations: services.AppAllowedOperations,
		pluginDefs:           map[string]*config.ProviderEntry{"slack": {}},
		invoker: struct {
			invocation.Invoker
			invocation.TokenResolver
		}{
			TokenResolver: &stubTokenResolver{token: "session-token", err: tokenErr},
		},
	}
	user := seedAppAccessTestUser(t, services, "catalog-credential@example.com")
	return server, &principal.Principal{
		SubjectID: principal.UserSubjectID(user.ID),
		UserID:    user.ID,
		Kind:      principal.KindUser,
	}
}

func serveAppAdminAllowedOperationsTestRequest(server *Server, p *principal.Principal) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/apps/slack/admin/allowed-operations", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "slack")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	req = req.WithContext(principal.WithPrincipal(ctx, p))
	recorder := httptest.NewRecorder()
	server.appAdminAllowedOperationsHandler(recorder, req)
	return recorder
}

func assertCatalogCredentialResponse(t *testing.T, endpoint string, response *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("%s status = %d, want %d: %s", endpoint, response.Code, wantStatus, response.Body.String())
	}
	var body struct {
		Code        string `json:"code"`
		Integration string `json:"integration"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("%s decode: %v", endpoint, err)
	}
	if body.Code != wantCode {
		t.Fatalf("%s code = %q, want %q", endpoint, body.Code, wantCode)
	}
	if wantCode != "" && body.Integration != "slack" {
		t.Fatalf("%s integration = %q, want slack", endpoint, body.Integration)
	}
}
