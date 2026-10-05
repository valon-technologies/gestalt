package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
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
			t.Fatalf("enabled operations = %#v, want all catalog defaults regardless of read-only hints", body.EnabledOperations)
		}
		if got := body.Operations[0].Title; got != "Chat Post Message" {
			t.Fatalf("fallback operation title = %q, want human-readable title", got)
		}
		if !body.Operations[1].ReadOnly {
			t.Fatal("conversations.list readOnly = false, want read-only hint preserved as metadata")
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

	t.Run("PUT ignores unknown operation", func(t *testing.T) {
		t.Parallel()
		server, alicePrincipal, _ := newAppAccessTestFixture(t)
		response := serveAppAccessTestRequest(t, server, http.MethodPut, map[string]any{
			"enabledOperations": []string{"missing.operation"},
		}, alicePrincipal)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
		}
		var body appAccessResponse
		decodeAppAccessTestResponse(t, response, &body)
		if len(body.EnabledOperations) != 0 {
			t.Fatalf("enabled operations = %#v, want none: the unknown id is dropped and every real operation was left off", body.EnabledOperations)
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

func TestAppAccessHandlersUseSessionCatalogWithEmptyProfile(t *testing.T) {
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
	if err := server.ensureAppAccessDefaults(context.Background(), credentialMaterial{
		SubjectID:   p.SubjectID,
		Integration: "slack",
	}, provider); err != nil {
		t.Fatalf("ensureAppAccessDefaults: %v", err)
	}
	profile, err := services.AppAccessProfiles.GetAppAccessProfile(context.Background(), p.SubjectID, "slack")
	if err != nil {
		t.Fatalf("profile after connect: %v", err)
	}
	if len(profile.DisabledOperations) != 0 || len(profile.ExtraOperations) != 0 || profile.Legacy {
		t.Fatalf("profile after connect = %#v, want empty overrides so session operations follow defaults", profile)
	}

	response := serveAppAccessTestRequest(t, server, http.MethodGet, nil, p)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body appAccessResponse
	decodeAppAccessTestResponse(t, response, &body)
	if len(body.Operations) != 1 || body.Operations[0].ID != "dynamic.list" || !body.Operations[0].Enabled {
		t.Fatalf("session catalog response = %#v, want dynamic operation enabled by default", body)
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

func TestAppAccessAdminBaselineKeepsNonAPIOperations(t *testing.T) {
	t.Parallel()

	disabled := catalog.APIExposurePrivate
	provider := &coretesting.StubIntegration{
		N: "example",
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "public"},
			{ID: "mcp-only", API: &disabled},
		}},
	}
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	baseline, err := server.appAccessBaselineCatalog(request, "example", provider)
	if err != nil {
		t.Fatalf("appAccessBaselineCatalog: %v", err)
	}
	if _, ok := catalog.OperationByID(baseline, "mcp-only"); !ok {
		t.Fatal("admin baseline omitted MCP-only operation")
	}

	public, err := server.appAccessCatalog(request, "example", provider)
	if err != nil {
		t.Fatalf("appAccessCatalog: %v", err)
	}
	if _, ok := catalog.OperationByID(public, "mcp-only"); ok {
		t.Fatal("public API catalog included API-disabled operation")
	}
}

func TestEnsureAppAccessDefaultsCanonicalizesOpaqueCredentialSubject(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	user := seedAppAccessTestUser(t, services, "canonical@example.com")
	identityJSON, err := json.Marshal(accountIdentity{Facts: []identityFact{{Kind: "email", Value: user.Email}}})
	if err != nil {
		t.Fatalf("marshal account identity: %v", err)
	}
	metadataJSON, err := json.Marshal(map[string]string{core.AccountIdentityMetadataKey: string(identityJSON)})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	provider := &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "conversations.list", Method: http.MethodGet},
		}},
	}
	server := &Server{users: services.Users, appAccessProfiles: services.AppAccessProfiles}
	if err := server.ensureAppAccessDefaults(context.Background(), credentialMaterial{
		SubjectID:    principal.UserSubjectID("auth0|opaque-user"),
		Integration:  "slack",
		MetadataJSON: string(metadataJSON),
	}, provider); err != nil {
		t.Fatalf("ensureAppAccessDefaults: %v", err)
	}
	profile, err := services.AppAccessProfiles.GetAppAccessProfile(context.Background(), principal.UserSubjectID(user.ID), "slack")
	if err != nil {
		t.Fatalf("canonical profile: %v", err)
	}
	if got := profile.EnabledOperations(provider.CatalogVal, core.AppAccessDefaultsFor(provider)); len(got) != 1 || got[0] != "conversations.list" {
		t.Fatalf("canonical profile operations = %#v, want conversations.list", got)
	}
	if _, err := services.AppAccessProfiles.GetAppAccessProfile(context.Background(), principal.UserSubjectID("auth0|opaque-user"), "slack"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("raw opaque profile = %v, want core.ErrNotFound", err)
	}
}

func TestEnsureAppAccessDefaultsKeepsEmailSubjectOwner(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	owner := seedAppAccessTestUser(t, services, "owner@example.com")
	connectedAccount := seedAppAccessTestUser(t, services, "connected@example.com")
	identityJSON, err := json.Marshal(accountIdentity{Facts: []identityFact{{Kind: "email", Value: connectedAccount.Email}}})
	if err != nil {
		t.Fatalf("marshal account identity: %v", err)
	}
	metadataJSON, err := json.Marshal(map[string]string{core.AccountIdentityMetadataKey: string(identityJSON)})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	provider := &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "conversations.list", Method: http.MethodGet},
		}},
	}
	server := &Server{users: services.Users, appAccessProfiles: services.AppAccessProfiles}
	if err := server.ensureAppAccessDefaults(context.Background(), credentialMaterial{
		SubjectID:    principal.UserSubjectID("owner@example.com"),
		Integration:  "slack",
		MetadataJSON: string(metadataJSON),
	}, provider); err != nil {
		t.Fatalf("ensureAppAccessDefaults: %v", err)
	}
	if _, err := services.AppAccessProfiles.GetAppAccessProfile(context.Background(), principal.UserSubjectID(owner.ID), "slack"); err != nil {
		t.Fatalf("owner profile: %v", err)
	}
	if _, err := services.AppAccessProfiles.GetAppAccessProfile(context.Background(), principal.UserSubjectID(connectedAccount.ID), "slack"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("connected-account profile = %v, want core.ErrNotFound", err)
	}
}

const (
	appAccessTestApp         = "slack"
	appAccessPostMessageOp   = "chat.postMessage"
	appAccessConversationsOp = "conversations.list"
	appAccessNewOp           = "files.upload"
)

// appAccessCatalogFixture exposes the provider so a test can add operations
// to the app after a user's profile already exists.
type appAccessCatalogFixture struct {
	server   *Server
	provider *coretesting.StubIntegration
	alice    *principal.Principal
	db       *coretesting.StubIndexedDB
	services *testutil.Services
}

func newAppAccessCatalogFixture(t *testing.T) *appAccessCatalogFixture {
	t.Helper()
	db := &coretesting.StubIndexedDB{}
	services, err := coredata.New(db)
	if err != nil {
		t.Fatalf("coredata.New: %v", err)
	}
	provider := &coretesting.StubIntegration{
		N:        appAccessTestApp,
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: appAccessPostMessageOp, Method: http.MethodPost},
			{ID: appAccessConversationsOp, Method: http.MethodGet},
		}},
	}
	server := &Server{
		providers:         testutil.NewProviderRegistry(t, provider),
		users:             services.Users,
		appAccessProfiles: services.AppAccessProfiles,
	}
	alice := seedAppAccessTestUser(t, services, "alice@example.com")
	return &appAccessCatalogFixture{
		server:   server,
		provider: provider,
		alice: &principal.Principal{
			SubjectID: principal.UserSubjectID(alice.ID),
			UserID:    alice.ID,
			Kind:      principal.KindUser,
		},
		db:       db,
		services: services,
	}
}

func (f *appAccessCatalogFixture) addOperation(id string) {
	f.provider.CatalogVal.Operations = append(f.provider.CatalogVal.Operations, catalog.CatalogOperation{ID: id, Method: http.MethodPost})
}

func (f *appAccessCatalogFixture) get(t *testing.T) appAccessResponse {
	t.Helper()
	response := serveAppAccessTestRequest(t, f.server, http.MethodGet, nil, f.alice)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body appAccessResponse
	decodeAppAccessTestResponse(t, response, &body)
	return body
}

func (f *appAccessCatalogFixture) put(t *testing.T, enabled []string) appAccessResponse {
	t.Helper()
	response := serveAppAccessTestRequest(t, f.server, http.MethodPut, map[string]any{"enabledOperations": enabled}, f.alice)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body appAccessResponse
	decodeAppAccessTestResponse(t, response, &body)
	return body
}

func (f *appAccessCatalogFixture) writeLegacyProfile(t *testing.T, enabled []string) {
	t.Helper()
	ctx := context.Background()
	if err := f.services.AppAccessProfiles.EnsureStore(ctx); err != nil {
		t.Fatalf("EnsureStore: %v", err)
	}
	raw, err := json.Marshal(enabled)
	if err != nil {
		t.Fatalf("marshal legacy list: %v", err)
	}
	err = f.db.ObjectStore(coredata.StoreAppAccessProfiles).Put(ctx, idb.Record{
		"id":                   f.alice.SubjectID + "\x1f" + appAccessTestApp,
		"subject_id":           f.alice.SubjectID,
		"app":                  appAccessTestApp,
		"enabled_operations":   string(raw),
		"defaults_initialized": true,
		"updated_at":           time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("write legacy profile: %v", err)
	}
}

func TestAppAccessFollowsAppDefaultsAsCatalogChanges(t *testing.T) {
	t.Parallel()

	t.Run("operation added after the profile existed is on by default", func(t *testing.T) {
		t.Parallel()
		f := newAppAccessCatalogFixture(t)
		if _, err := f.services.AppAccessProfiles.EnsureAppAccessDefaults(context.Background(), f.alice.SubjectID, appAccessTestApp); err != nil {
			t.Fatalf("EnsureAppAccessDefaults: %v", err)
		}
		f.put(t, []string{appAccessPostMessageOp, appAccessConversationsOp})

		f.addOperation(appAccessNewOp)

		body := f.get(t)
		want := []string{appAccessPostMessageOp, appAccessConversationsOp, appAccessNewOp}
		slices.Sort(want)
		if !slices.Equal(body.EnabledOperations, want) {
			t.Fatalf("enabled = %#v, want %#v including the new default-on operation", body.EnabledOperations, want)
		}
		var listed bool
		for _, op := range body.Operations {
			if op.ID == appAccessNewOp {
				listed = true
				if !op.Enabled {
					t.Fatalf("new operation enabled = false, want true: %#v", op)
				}
			}
		}
		if !listed {
			t.Fatalf("operations = %#v, want the new operation listed", body.Operations)
		}
	})

	t.Run("opt-out survives a new operation", func(t *testing.T) {
		t.Parallel()
		f := newAppAccessCatalogFixture(t)
		f.put(t, []string{appAccessConversationsOp})

		f.addOperation(appAccessNewOp)

		body := f.get(t)
		want := []string{appAccessConversationsOp, appAccessNewOp}
		slices.Sort(want)
		if !slices.Equal(body.EnabledOperations, want) {
			t.Fatalf("enabled = %#v, want %#v: %s stays off, the new operation follows its default", body.EnabledOperations, want, appAccessPostMessageOp)
		}
	})

	t.Run("PUT with a stale id still saves the real toggle", func(t *testing.T) {
		t.Parallel()
		f := newAppAccessCatalogFixture(t)

		body := f.put(t, []string{appAccessConversationsOp, "removed.operation"})

		if !slices.Equal(body.EnabledOperations, []string{appAccessConversationsOp}) {
			t.Fatalf("enabled = %#v, want only %s", body.EnabledOperations, appAccessConversationsOp)
		}
		if got := f.get(t).EnabledOperations; !slices.Equal(got, []string{appAccessConversationsOp}) {
			t.Fatalf("persisted enabled = %#v, want only %s", got, appAccessConversationsOp)
		}
	})

	t.Run("opt-out survives its operation being absent during another save", func(t *testing.T) {
		t.Parallel()
		f := newAppAccessCatalogFixture(t)
		f.put(t, []string{appAccessConversationsOp})

		removed := f.provider.CatalogVal.Operations[0]
		f.provider.CatalogVal.Operations = f.provider.CatalogVal.Operations[1:]
		f.put(t, []string{appAccessConversationsOp})
		f.provider.CatalogVal.Operations = append(f.provider.CatalogVal.Operations, removed)

		if got := f.get(t).EnabledOperations; !slices.Equal(got, []string{appAccessConversationsOp}) {
			t.Fatalf("enabled = %#v, want %s to stay off after reappearing", got, removed.ID)
		}
	})

	t.Run("legacy profile keeps its exact allow list until the user saves", func(t *testing.T) {
		t.Parallel()
		f := newAppAccessCatalogFixture(t)
		f.writeLegacyProfile(t, []string{appAccessConversationsOp})
		f.addOperation(appAccessNewOp)

		body := f.get(t)
		if !body.DefaultsInitialized || !slices.Equal(body.EnabledOperations, []string{appAccessConversationsOp}) {
			t.Fatalf("legacy response = %#v, want exactly the old allow list", body)
		}
		stored, err := f.services.AppAccessProfiles.GetAppAccessProfile(context.Background(), f.alice.SubjectID, appAccessTestApp)
		if err != nil || !stored.Legacy {
			t.Fatalf("profile before save = %#v, %v, want legacy", stored, err)
		}

		f.put(t, []string{appAccessConversationsOp, appAccessNewOp})

		stored, err = f.services.AppAccessProfiles.GetAppAccessProfile(context.Background(), f.alice.SubjectID, appAccessTestApp)
		if err != nil {
			t.Fatalf("GetAppAccessProfile: %v", err)
		}
		if stored.Legacy || !slices.Equal(stored.DisabledOperations, []string{appAccessPostMessageOp}) || len(stored.ExtraOperations) != 0 {
			t.Fatalf("profile after save = %#v, want converted to overrides disabling %s", stored, appAccessPostMessageOp)
		}
		f.addOperation("another.operation")
		want := []string{appAccessConversationsOp, appAccessNewOp, "another.operation"}
		slices.Sort(want)
		if got := f.get(t).EnabledOperations; !slices.Equal(got, want) {
			t.Fatalf("enabled after conversion = %#v, want %#v", got, want)
		}
	})
}

func newAppAccessTestFixture(t *testing.T) (*Server, *principal.Principal, *principal.Principal) {
	t.Helper()
	services := testutil.NewStubServices(t)
	readOnly := true
	provider := &coretesting.StubIntegration{
		N:        "slack",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{Operations: []catalog.CatalogOperation{
			{ID: "chat.postMessage", Method: http.MethodPost},
			{
				ID:          "conversations.list",
				Method:      http.MethodGet,
				Annotations: catalog.CapabilityAnnotations{ReadOnlyHint: &readOnly},
			},
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
	req.Header.Set("Content-Type", "application/json")
	server.appAccessHandler(recorder, req)
	return recorder
}

func decodeAppAccessTestResponse(t *testing.T, response *httptest.ResponseRecorder, dst *appAccessResponse) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}
