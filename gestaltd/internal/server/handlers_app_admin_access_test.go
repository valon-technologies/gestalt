package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func appAdminAccessTestServer(t *testing.T) (*Server, *testutil.Services) {
	t.Helper()
	services := testutil.NewStubServices(t)
	private := catalog.APIExposurePrivate
	mcpDisabled := false
	provider := &stubCatalogProvider{
		stubProvider: stubProvider{name: "front-porch", connMode: core.ConnectionModeNone},
		cat: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "versions", Method: http.MethodGet},
			{ID: "setVersion", Method: http.MethodPost},
			{ID: "unsafeSetVersion", Method: http.MethodPost, API: &private, MCP: &mcpDisabled},
		}},
	}
	return &Server{
		providers:         testutil.NewProviderRegistry(t, provider),
		users:             services.Users,
		appAccessProfiles: services.AppAccessProfiles,
	}, services
}

func serveAppAdminAccessRequest(t *testing.T, server *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/apps/front-porch/admin/access?"+query, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("app", "front-porch")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	recorder := httptest.NewRecorder()
	server.getAppAdminAccess(recorder, req)
	return recorder
}

func decodeAppAdminAccess(t *testing.T, recorder *httptest.ResponseRecorder) appAdminAccessResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body appAdminAccessResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func TestAppAdminAccessReportsSavedProfile(t *testing.T) {
	t.Parallel()

	server, services := appAdminAccessTestServer(t)
	user := seedAppAccessTestUser(t, services, "saver@example.com")
	// Opt out of everything but versions, including the private operation an
	// admin cannot otherwise see withheld.
	if _, err := services.AppAccessProfiles.SetAppAccessOverrides(
		context.Background(),
		principal.UserSubjectID(user.ID),
		"front-porch",
		[]string{"setVersion", "unsafeSetVersion"},
		nil,
	); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}

	body := decodeAppAdminAccess(t, serveAppAdminAccessRequest(t, server, "email=saver@example.com"))
	if !body.ProfileExists {
		t.Fatalf("profileExists = false, want true: %#v", body)
	}
	if body.SubjectID != principal.UserSubjectID(user.ID) {
		t.Fatalf("subjectId = %q, want the profile key", body.SubjectID)
	}
	if len(body.EnabledOperations) != 1 || body.EnabledOperations[0] != "versions" {
		t.Fatalf("enabledOperations = %#v, want [versions]", body.EnabledOperations)
	}
	// The private operation is the one an admin cannot otherwise see withheld.
	want := []string{"setVersion", "unsafeSetVersion"}
	if len(body.DeniedOperations) != len(want) {
		t.Fatalf("deniedOperations = %#v, want %#v", body.DeniedOperations, want)
	}
	for i, operation := range want {
		if body.DeniedOperations[i] != operation {
			t.Fatalf("deniedOperations = %#v, want %#v", body.DeniedOperations, want)
		}
	}
}

func TestAppAdminAccessIgnoresDefaultOnOperationsAddedAfterOptOut(t *testing.T) {
	t.Parallel()

	server, services := appAdminAccessTestServer(t)
	user := seedAppAccessTestUser(t, services, "optout@example.com")
	if _, err := services.AppAccessProfiles.SetAppAccessOverrides(
		context.Background(),
		principal.UserSubjectID(user.ID),
		"front-porch",
		[]string{"removedOperation", "unsafeSetVersion"},
		nil,
	); err != nil {
		t.Fatalf("SetAppAccessOverrides: %v", err)
	}

	body := decodeAppAdminAccess(t, serveAppAdminAccessRequest(t, server, "email=optout@example.com"))
	if !slices.Equal(body.EnabledOperations, []string{"setVersion", "versions"}) {
		t.Fatalf("enabledOperations = %#v, want every current default-on operation", body.EnabledOperations)
	}
	if !slices.Equal(body.DeniedOperations, []string{"unsafeSetVersion"}) {
		t.Fatalf("deniedOperations = %#v, want only the opted-out operation", body.DeniedOperations)
	}
}

func TestAppAdminAccessReportsMissingProfile(t *testing.T) {
	t.Parallel()

	server, services := appAdminAccessTestServer(t)
	seedAppAccessTestUser(t, services, "untouched@example.com")

	body := decodeAppAdminAccess(t, serveAppAdminAccessRequest(t, server, "email=untouched@example.com"))
	if body.ProfileExists {
		t.Fatalf("profileExists = true, want false: %#v", body)
	}
	if len(body.DeniedOperations) != 0 {
		t.Fatalf("deniedOperations = %#v, want none when no profile gates the user", body.DeniedOperations)
	}
}

func TestAppAdminAccessRequiresSubjectSelector(t *testing.T) {
	t.Parallel()

	server, _ := appAdminAccessTestServer(t)
	recorder := serveAppAdminAccessRequest(t, server, "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestAppAdminAccessRejectsUnknownEmail(t *testing.T) {
	t.Parallel()

	server, _ := appAdminAccessTestServer(t)
	recorder := serveAppAdminAccessRequest(t, server, "email=nobody@example.com")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
