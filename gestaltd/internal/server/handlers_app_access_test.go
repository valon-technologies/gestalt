package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func TestAppAccessHandlers(t *testing.T) {
	t.Parallel()

	t.Run("GET returns catalog defaults", func(t *testing.T) {
		t.Parallel()
		server, alicePrincipal, _ := newAppAccessTestFixture(t)
		response := serveAppAccessTestRequest(t, server, http.MethodGet, nil, alicePrincipal)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var body appAccessResponse
		decodeAppAccessTestResponse(t, response, &body)
		if body.DefaultsInitialized {
			t.Fatal("defaultsInitialized = true before a profile was persisted")
		}
		if len(body.EnabledOperations) != 2 {
			t.Fatalf("enabled operations = %#v, want catalog defaults", body.EnabledOperations)
		}
		if got := body.Operations[0].Title; got != "Chat Post Message" {
			t.Fatalf("fallback operation title = %q, want human-readable title", got)
		}
	})

	t.Run("PUT updates and persists allow list", func(t *testing.T) {
		t.Parallel()
		server, alicePrincipal, _ := newAppAccessTestFixture(t)
		response := serveAppAccessTestRequest(t, server, http.MethodPut, map[string]any{
			"enabledOperations": []string{"conversations.list"},
		}, alicePrincipal)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var body appAccessResponse
		decodeAppAccessTestResponse(t, response, &body)
		if !body.DefaultsInitialized || len(body.EnabledOperations) != 1 || body.EnabledOperations[0] != "conversations.list" {
			t.Fatalf("updated response = %#v, want persisted read-only allow list", body)
		}
	})

	t.Run("PUT empty list disables all operations", func(t *testing.T) {
		t.Parallel()
		server, alicePrincipal, _ := newAppAccessTestFixture(t)
		response := serveAppAccessTestRequest(t, server, http.MethodPut, map[string]any{
			"enabledOperations": []string{},
		}, alicePrincipal)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var body appAccessResponse
		decodeAppAccessTestResponse(t, response, &body)
		if len(body.EnabledOperations) != 0 {
			t.Fatalf("enabled operations = %#v, want empty", body.EnabledOperations)
		}
	})

	t.Run("PUT rejects unknown operation", func(t *testing.T) {
		t.Parallel()
		server, alicePrincipal, _ := newAppAccessTestFixture(t)
		response := serveAppAccessTestRequest(t, server, http.MethodPut, map[string]any{
			"enabledOperations": []string{"missing.operation"},
		}, alicePrincipal)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
		}
	})

	t.Run("profile is isolated by canonical subject", func(t *testing.T) {
		t.Parallel()
		server, alicePrincipal, bobPrincipal := newAppAccessTestFixture(t)
		update := serveAppAccessTestRequest(t, server, http.MethodPut, map[string]any{
			"enabledOperations": []string{},
		}, alicePrincipal)
		if update.Code != http.StatusOK {
			t.Fatalf("alice update status = %d, want %d: %s", update.Code, http.StatusOK, update.Body.String())
		}
		response := serveAppAccessTestRequest(t, server, http.MethodGet, nil, bobPrincipal)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var body appAccessResponse
		decodeAppAccessTestResponse(t, response, &body)
		if body.DefaultsInitialized || len(body.EnabledOperations) != 2 {
			t.Fatalf("bob response = %#v, want independent defaults", body)
		}
	})

	t.Run("requires authenticated user", func(t *testing.T) {
		t.Parallel()
		server, _, _ := newAppAccessTestFixture(t)
		response := serveAppAccessTestRequest(t, server, http.MethodGet, nil, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
		}
	})
}

func TestAppAccessHandlersUseSessionCatalogBeforeInitializingProfile(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	provider := &stubSessionProvider{
		stubCatalogProvider: stubCatalogProvider{
			stubProvider: stubProvider{
				name:     "slack",
				connMode: core.ConnectionModeSubject,
			},
		},
		sessionCat: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "dynamic.list", Method: http.MethodGet},
		}},
	}
	server := &Server{
		providers:         testutil.NewProviderRegistry(t, provider),
		users:             services.Users,
		appAccessProfiles: services.AppAccessProfiles,
		invoker: struct {
			invocation.Invoker
			invocation.TokenResolver
		}{
			TokenResolver: &stubTokenResolver{token: "session-token"},
		},
	}
	user := seedAppAccessTestUser(t, services, "dynamic@example.com")
	p := &principal.Principal{
		SubjectID: principal.UserSubjectID(user.ID),
		UserID:    user.ID,
		Kind:      principal.KindUser,
	}
	if err := server.ensureAppAccessDefaults(context.Background(), p.SubjectID, "slack", provider); err != nil {
		t.Fatalf("ensureAppAccessDefaults: %v", err)
	}
	if _, err := services.AppAccessProfiles.GetAppAccessProfile(context.Background(), p.SubjectID, "slack"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("profile after empty static catalog = %v, want core.ErrNotFound", err)
	}

	response := serveAppAccessTestRequest(t, server, http.MethodGet, nil, p)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body appAccessResponse
	decodeAppAccessTestResponse(t, response, &body)
	if len(body.Operations) != 1 || body.Operations[0].ID != "dynamic.list" || body.DefaultsInitialized {
		t.Fatalf("session catalog response = %#v, want dynamic operation without initialized profile", body)
	}

	response = serveAppAccessTestRequest(t, server, http.MethodPut, map[string]any{
		"enabledOperations": []string{"dynamic.list"},
	}, p)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	decodeAppAccessTestResponse(t, response, &body)
	if !body.DefaultsInitialized || len(body.EnabledOperations) != 1 || body.EnabledOperations[0] != "dynamic.list" {
		t.Fatalf("session catalog update = %#v, want persisted dynamic operation", body)
	}
}

func newAppAccessTestFixture(t *testing.T) (*Server, *principal.Principal, *principal.Principal) {
	t.Helper()
	services := testutil.NewStubServices(t)
	provider := &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "chat.postMessage", Method: http.MethodPost},
			{ID: "conversations.list", Method: http.MethodGet},
		}},
	}
	server := &Server{
		providers:         testutil.NewProviderRegistry(t, provider),
		users:             services.Users,
		appAccessProfiles: services.AppAccessProfiles,
	}
	alice := seedAppAccessTestUser(t, services, "alice@example.com")
	bob := seedAppAccessTestUser(t, services, "bob@example.com")
	return server, &principal.Principal{
			SubjectID: principal.UserSubjectID(alice.ID),
			UserID:    alice.ID,
			Kind:      principal.KindUser,
		}, &principal.Principal{
			SubjectID: principal.UserSubjectID(bob.ID),
			UserID:    bob.ID,
			Kind:      principal.KindUser,
		}
}

func seedAppAccessTestUser(t *testing.T, services *testutil.Services, email string) *core.User {
	t.Helper()
	user, err := services.Users.FindOrCreateUser(context.Background(), email)
	if err != nil {
		t.Fatalf("FindOrCreateUser(%q): %v", email, err)
	}
	return user
}

func serveAppAccessTestRequest(t *testing.T, server *Server, method string, payload map[string]any, p *principal.Principal) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, "/apps/slack/access", body)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("name", "slack")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx)
	if p != nil {
		ctx = principal.WithPrincipal(ctx, p)
	}
	req = req.WithContext(ctx)
	recorder := httptest.NewRecorder()
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
		server.updateAppAccess(recorder, req)
	} else {
		server.getAppAccess(recorder, req)
	}
	return recorder
}

func decodeAppAccessTestResponse(t *testing.T, response *httptest.ResponseRecorder, dst *appAccessResponse) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}
