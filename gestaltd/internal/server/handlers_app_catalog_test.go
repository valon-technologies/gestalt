package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type errorListCredentials struct {
	core.ExternalCredentialProvider
}

func (e errorListCredentials) ListCredentials(ctx context.Context, subject, audience string) ([]*core.ExternalCredential, error) {
	return nil, fmt.Errorf("credentials unavailable")
}

func TestAppCatalogOmitsSubjectConnectionState(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	recording := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
	svc.ExternalCredentials = recording
	u := seedUser(t, svc, "anonymous@gestalt")
	seedToken(t, svc, &core.ExternalCredential{
		ID:        "tok1",
		Subject:   principal.UserSubjectID(u.ID),
		Audience:  "slack:default",
		Qualifier: "default",
		Grant:     &core.ExternalCredentialGrant{AccessToken: "test-token"},
	})

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack", Desc: "Team messaging"})
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	catalog := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if recording.listCredentialsCalls.Load() != 0 {
		t.Fatalf("catalog listing consulted subject credentials %d times", recording.listCredentialsCalls.Load())
	}

	var catalogApps []map[string]any
	if err := json.Unmarshal(catalog, &catalogApps); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalogApps) != 1 {
		t.Fatalf("catalog apps = %d, want 1: %s", len(catalogApps), catalog)
	}
	app := catalogApps[0]
	if app["name"] != "slack" {
		t.Fatalf("catalog name = %#v, want slack", app["name"])
	}
	for _, field := range []string{"status", "credentialState", "healthState", "actions", "iconSvg"} {
		if _, ok := app[field]; ok {
			t.Fatalf("catalog must not include subject or icon bytes field %q: %#v", field, app)
		}
	}
	connections, _ := app["connections"].([]any)
	if len(connections) == 0 {
		t.Fatalf("catalog missing connection schema: %#v", app)
	}
	connection, _ := connections[0].(map[string]any)
	for _, field := range []string{"status", "instances", "connected", "preferredInstance"} {
		if _, ok := connection[field]; ok {
			t.Fatalf("catalog connection schema must not include %q: %#v", field, connection)
		}
	}

	overlay := getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusOK, "")
	if recording.listCredentialsCalls.Load() == 0 {
		t.Fatal("connection overlay did not list subject credentials")
	}
	var statuses []struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
		Connected       bool   `json:"connected"`
		Connections     []struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(overlay, &statuses); err != nil {
		t.Fatalf("decode overlay: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Name != "slack" {
		t.Fatalf("overlay = %s", overlay)
	}
	if statuses[0].Status != "ready" || statuses[0].CredentialState != "connected" {
		t.Fatalf("overlay status = {%q, %q}, want ready/connected", statuses[0].Status, statuses[0].CredentialState)
	}
	if !statuses[0].Connected {
		t.Fatalf("overlay app connected = false, want true: %s", overlay)
	}
	if len(statuses[0].Connections) != 1 || !statuses[0].Connections[0].Connected {
		t.Fatalf("overlay connections = %+v", statuses[0].Connections)
	}
	catalogConn, _ := connection["name"].(string)
	if catalogConn == "" || catalogConn != statuses[0].Connections[0].Name {
		t.Fatalf("catalog connection %q does not zip with overlay %q", catalogConn, statuses[0].Connections[0].Name)
	}

	composed := getJSONPath(t, ts, "/api/v1/apps", http.StatusOK, "")
	var integrations []struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
		Connected       bool   `json:"connected"`
	}
	if err := json.Unmarshal(composed, &integrations); err != nil {
		t.Fatalf("decode composed listing: %v", err)
	}
	if len(integrations) != 1 || integrations[0].Name != "slack" {
		t.Fatalf("composed listing = %s", composed)
	}
	if integrations[0].Status != "ready" || integrations[0].CredentialState != "connected" {
		t.Fatalf("composed status = {%q, %q}, want ready/connected", integrations[0].Status, integrations[0].CredentialState)
	}
	if !integrations[0].Connected {
		t.Fatalf("composed listing app connected = false, want true: %s", composed)
	}
}

func TestAppCatalogSucceedsWhenSubjectCredentialsFail(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	seedUser(t, svc, "anonymous@gestalt")
	svc.ExternalCredentials = errorListCredentials{ExternalCredentialProvider: svc.ExternalCredentials}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack"})
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	catalog := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var catalogApps []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(catalog, &catalogApps); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalogApps) != 1 || catalogApps[0].Name != "slack" {
		t.Fatalf("catalog = %s", catalog)
	}

	if body := getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusInternalServerError, ""); !jsonContainsError(body, "failed to check integration status") {
		t.Fatalf("overlay error = %s", body)
	}
	if body := getJSONPath(t, ts, "/api/v1/apps", http.StatusInternalServerError, ""); !jsonContainsError(body, "failed to check integration status") {
		t.Fatalf("composed error = %s", body)
	}
}

func TestAppCatalogServesIconsByURL(t *testing.T) {
	t.Parallel()

	const testSVG = `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/></svg>`
	prov, err := declarative.Build(&declarative.Definition{
		Provider:    "iconprov",
		DisplayName: "Icon Provider",
		Description: "Has an icon",
		IconSVG:     testSVG,
		BaseURL:     "https://api.example.com",
		Auth:        declarative.AuthDef{Type: "manual"},
		Operations: map[string]declarative.OperationDef{
			"op": {Description: "An op", Method: http.MethodGet, Path: "/op"},
		},
	}, declarative.ConnectionDef{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	svc := testutil.NewStubServices(t)
	recording := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
	svc.ExternalCredentials = recording

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, prov)
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	iconReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/catalog/apps/iconprov/icon", nil)
	if err != nil {
		t.Fatalf("new icon request: %v", err)
	}
	iconResp, err := http.DefaultClient.Do(iconReq)
	if err != nil {
		t.Fatalf("GET icon: %v", err)
	}
	defer func() { _ = iconResp.Body.Close() }()
	iconBody, err := io.ReadAll(iconResp.Body)
	if err != nil {
		t.Fatalf("read icon: %v", err)
	}
	if iconResp.StatusCode != http.StatusOK {
		t.Fatalf("icon status = %d: %s", iconResp.StatusCode, iconBody)
	}
	if recording.listCredentialsCalls.Load() != 0 {
		t.Fatalf("icon lookup listed subject credentials %d times", recording.listCredentialsCalls.Load())
	}
	if got := iconResp.Header.Get("Content-Type"); got != "image/svg+xml; charset=utf-8" {
		t.Fatalf("icon content type = %q", got)
	}
	if string(iconBody) != testSVG {
		t.Fatalf("icon body = %q, want %q", iconBody, testSVG)
	}

	catalog := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var catalogApps []struct {
		Name    string `json:"name"`
		IconURL string `json:"iconUrl"`
		IconSVG string `json:"iconSvg"`
	}
	if err := json.Unmarshal(catalog, &catalogApps); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalogApps) != 1 {
		t.Fatalf("catalog = %s", catalog)
	}
	if catalogApps[0].IconSVG != "" {
		t.Fatalf("catalog inlined icon bytes: %q", catalogApps[0].IconSVG)
	}
	wantURL := "/api/v1/catalog/apps/iconprov/icon"
	if catalogApps[0].IconURL != wantURL {
		t.Fatalf("iconUrl = %q, want %q", catalogApps[0].IconURL, wantURL)
	}

	composed := getJSONPath(t, ts, "/api/v1/apps", http.StatusOK, "")
	var integrations []struct {
		IconSVG string `json:"iconSvg"`
	}
	if err := json.Unmarshal(composed, &integrations); err != nil {
		t.Fatalf("decode composed listing: %v", err)
	}
	if len(integrations) != 1 || integrations[0].IconSVG != testSVG {
		t.Fatalf("composed listing lost iconSvg compatibility: %s", composed)
	}
}

func TestAppCatalogIncludesSourceTreeURL(t *testing.T) {
	t.Parallel()

	githubApp := &coretesting.StubIntegration{N: "roadmap", DN: "Roadmap"}
	gitlabApp := &coretesting.StubIntegration{N: "notes", DN: "Notes"}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, githubApp, gitlabApp)
		defs := testPluginDefsForConnections("roadmap", "default")
		defs["roadmap"].Source = config.ProviderSource{
			Git: &config.GitSourceDef{
				Repo: "https://github.com/example/apps.git",
				Ref:  "main",
				Path: "apps/roadmap/manifest.yaml",
			},
		}
		notes := testPluginDefsForConnections("notes", "default")["notes"]
		notes.Source = config.ProviderSource{
			Git: &config.GitSourceDef{
				Repo: "https://gitlab.example.com/group/app.git",
				Ref:  "main",
				Path: "apps/notes/manifest.yaml",
			},
		}
		defs["notes"] = notes
		cfg.AppDefs = defs
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	catalog := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var catalogApps []map[string]any
	if err := json.Unmarshal(catalog, &catalogApps); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	got := map[string]map[string]any{}
	for _, app := range catalogApps {
		name, _ := app["name"].(string)
		got[name] = app
	}
	roadmap, ok := got["roadmap"]
	if !ok {
		t.Fatalf("expected roadmap in catalog, got %s", catalog)
	}
	wantGitHub := "https://github.com/example/apps/tree/main/apps/roadmap"
	if gotTree, _ := roadmap["sourceTreeUrl"].(string); gotTree != wantGitHub {
		t.Fatalf("catalog roadmap sourceTreeUrl = %q, want %q: %s", gotTree, wantGitHub, catalog)
	}
	notes, ok := got["notes"]
	if !ok {
		t.Fatalf("expected notes in catalog, got %s", catalog)
	}
	if _, exists := notes["sourceTreeUrl"]; exists {
		t.Fatalf("catalog notes sourceTreeUrl = %+v, want omitted for non-GitHub git", notes["sourceTreeUrl"])
	}

	overlay := getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusOK, "")
	var statuses []map[string]any
	if err := json.Unmarshal(overlay, &statuses); err != nil {
		t.Fatalf("decode overlay: %v", err)
	}
	for _, status := range statuses {
		if _, exists := status["sourceTreeUrl"]; exists {
			t.Fatalf("connection overlay must not include catalog identity field sourceTreeUrl: %#v", status)
		}
	}

	composed := getJSONPath(t, ts, "/api/v1/apps", http.StatusOK, "")
	var integrations []map[string]any
	if err := json.Unmarshal(composed, &integrations); err != nil {
		t.Fatalf("decode composed listing: %v", err)
	}
	composedByName := map[string]map[string]any{}
	for _, integration := range integrations {
		name, _ := integration["name"].(string)
		composedByName[name] = integration
	}
	if gotTree, _ := composedByName["roadmap"]["sourceTreeUrl"].(string); gotTree != wantGitHub {
		t.Fatalf("composed roadmap sourceTreeUrl = %q, want %q: %s", gotTree, wantGitHub, composed)
	}
	if _, exists := composedByName["notes"]["sourceTreeUrl"]; exists {
		t.Fatalf("composed notes sourceTreeUrl = %+v, want omitted", composedByName["notes"]["sourceTreeUrl"])
	}
}

func TestAppCatalogOmitsCatalogHiddenApps(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: append(
			subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
			subjectSetGrant(subjectID, "viewer", "app", "otherApp")...,
		),
	}
	ts := listingTestServer(t, authz, subjectID)
	catalog := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "Bearer listing-token")
	var catalogApps []listedIntegration
	if err := json.Unmarshal(catalog, &catalogApps); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if _, ok := mountedPathFor(catalogApps, "publicHidden"); ok {
		t.Fatalf("catalog-hidden app leaked: %#v", catalogApps)
	}
	if mounted, ok := mountedPathFor(catalogApps, "sampleApp"); !ok || mounted == "" {
		t.Fatalf("granted app missing from catalog: %#v", catalogApps)
	}

	_ = getJSONPath(t, ts, "/api/v1/catalog/apps/publicHidden/icon", http.StatusNotFound, "Bearer listing-token")
}

func TestAppCatalogIncludesSchemaForRegistryOnlyApps(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "registry-only"),
		},
	}
	svc := testutil.NewStubServices(t)
	recording := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
	svc.ExternalCredentials = recording

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.Providers = testutil.NewProviderRegistry(t)
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"registry-only": {
				Source:      config.ProviderSource{Registry: "toolshed"},
				DisplayName: "Registry Only",
				Connections: map[string]*config.ConnectionDef{
					"workspace": {
						ConnectionID: "registry-only:workspace",
						DisplayName:  "Workspace",
						Mode:         providermanifestv1.ConnectionModeSubject,
						Auth: config.ConnectionAuthDef{
							Type: providermanifestv1.AuthTypeManual,
						},
					},
				},
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	catalog := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "Bearer alice-token")
	if recording.listCredentialsCalls.Load() != 0 {
		t.Fatalf("catalog listing consulted subject credentials %d times", recording.listCredentialsCalls.Load())
	}
	var catalogApps []map[string]any
	if err := json.Unmarshal(catalog, &catalogApps); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalogApps) != 1 || catalogApps[0]["name"] != "registry-only" {
		t.Fatalf("catalog = %s", catalog)
	}
	connections, _ := catalogApps[0]["connections"].([]any)
	if len(connections) != 1 {
		t.Fatalf("registry-only catalog missing connection schema: %s", catalog)
	}
	catalogConn, _ := connections[0].(map[string]any)
	if catalogConn["name"] != "workspace" {
		t.Fatalf("catalog connection = %#v, want workspace", catalogConn)
	}

	overlay := getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusOK, "Bearer alice-token")
	if recording.listCredentialsCalls.Load() == 0 {
		t.Fatal("connection overlay did not list subject credentials")
	}
	var statuses []struct {
		Name        string `json:"name"`
		Connections []struct {
			Name string `json:"name"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(overlay, &statuses); err != nil {
		t.Fatalf("decode overlay: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Name != "registry-only" {
		t.Fatalf("overlay = %s", overlay)
	}
	if len(statuses[0].Connections) != 1 || statuses[0].Connections[0].Name != "workspace" {
		t.Fatalf("overlay connections = %+v, want workspace so catalog and overlay zip", statuses[0].Connections)
	}
}

func TestAppOverlayNoAuthIsNotProductConnected(t *testing.T) {
	t.Parallel()

	svc := testutil.NewStubServices(t)
	recording := newRecordingExternalCredentialProvider(svc.ExternalCredentials)
	svc.ExternalCredentials = recording
	seedUser(t, svc, "anonymous@gestalt")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{
			N:        "no-auth",
			DN:       "No Auth",
			ConnMode: core.ConnectionModeNone,
		})
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"no-auth": {
				Connections: map[string]*config.ConnectionDef{
					"webhook": {
						ConnectionID: "no-auth:webhook",
						DisplayName:  "Webhook",
						Mode:         providermanifestv1.ConnectionModeNone,
					},
				},
			},
		}
		cfg.Services = svc
	})
	testutil.CloseOnCleanup(t, ts)

	overlay := getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusOK, "")
	if recording.listCredentialsCalls.Load() == 0 {
		t.Fatal("connection overlay did not list subject credentials")
	}
	var statuses []struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		CredentialState string `json:"credentialState"`
		Connected       bool   `json:"connected"`
		Connections     []struct {
			Name            string `json:"name"`
			Connected       bool   `json:"connected"`
			CredentialState string `json:"credentialState"`
			CredentialMode  string `json:"credentialMode"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(overlay, &statuses); err != nil {
		t.Fatalf("decode overlay: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Name != "no-auth" {
		t.Fatalf("overlay = %s", overlay)
	}
	if len(statuses[0].Connections) == 0 {
		t.Fatalf("overlay missing no-auth connection row: %s", overlay)
	}
	if statuses[0].CredentialState != "not_required" {
		t.Fatalf("overlay credential state = %q, want not_required: %s", statuses[0].CredentialState, overlay)
	}
	if statuses[0].Connected {
		t.Fatalf("no-auth overlay app must not be product-connected: %s", overlay)
	}
	for _, connection := range statuses[0].Connections {
		if connection.Connected {
			t.Fatalf("no-auth overlay connection %q must not be product-connected: %s", connection.Name, overlay)
		}
	}

	composed := getJSONPath(t, ts, "/api/v1/apps", http.StatusOK, "")
	var integrations []struct {
		Name        string `json:"name"`
		Connected   bool   `json:"connected"`
		Connections []struct {
			Connected bool `json:"connected"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(composed, &integrations); err != nil {
		t.Fatalf("decode composed listing: %v", err)
	}
	if len(integrations) != 1 || integrations[0].Name != "no-auth" {
		t.Fatalf("composed listing = %s", composed)
	}
	if integrations[0].Connected {
		t.Fatalf("composed no-auth app must not be product-connected: %s", composed)
	}
	for i, connection := range integrations[0].Connections {
		if connection.Connected {
			t.Fatalf("composed no-auth connection[%d] must not be product-connected: %s", i, composed)
		}
	}
}

type countingMissResolver struct {
	calls atomic.Int32
}

func (c *countingMissResolver) ResolveProvider(context.Context, string) (core.Provider, error) {
	c.calls.Add(1)
	return nil, core.ErrNotFound
}

func TestAppCatalogReusesTenantDirectoryAcrossRequests(t *testing.T) {
	t.Parallel()

	resolver := &countingMissResolver{}
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack"})
	providers.SetRemoteResolver(resolver)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	first := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	afterCatalog := resolver.calls.Load()
	if afterCatalog == 0 {
		t.Fatal("catalog snapshot never resolved providers")
	}
	second := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if resolver.calls.Load() != afterCatalog {
		t.Fatalf("second catalog rebuilt the tenant directory: resolver calls %d after first, %d after second", afterCatalog, resolver.calls.Load())
	}
	_ = getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusOK, "")
	afterOverlay := resolver.calls.Load()
	if afterOverlay <= afterCatalog {
		t.Fatal("connection overlay must resolve a live provider instead of reusing the snapshot handle")
	}
	_ = getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if resolver.calls.Load() != afterOverlay {
		t.Fatalf("catalog after overlay rebuilt the tenant directory: resolver calls %d after overlay, %d after later catalog", afterOverlay, resolver.calls.Load())
	}
	if string(first) != string(second) {
		t.Fatalf("cached catalog changed: %s vs %s", first, second)
	}
}

type requestScopedStubResolver struct {
	stub  *coretesting.StubIntegration
	calls atomic.Int32
}

type requestScopedStub struct {
	*coretesting.StubIntegration
	closed atomic.Bool
}

func (p *requestScopedStub) Close() error {
	p.closed.Store(true)
	return nil
}

func (p *requestScopedStub) ConnectionMode() core.ConnectionMode {
	if p.closed.Load() {
		panic("ConnectionMode on closed provider")
	}
	return p.StubIntegration.ConnectionMode()
}

func (r *requestScopedStubResolver) ResolveProvider(ctx context.Context, _ string) (core.Provider, error) {
	r.calls.Add(1)
	p := &requestScopedStub{StubIntegration: r.stub}
	context.AfterFunc(ctx, func() { _ = p.Close() })
	return p, nil
}

func TestAppCatalogDoesNotCacheRequestScopedProviders(t *testing.T) {
	t.Parallel()

	resolver := &requestScopedStubResolver{
		stub: &coretesting.StubIntegration{N: "slack", DN: "Slack", ConnMode: core.ConnectionModeNone},
	}
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Local Slack", ConnMode: core.ConnectionModeNone})
	providers.SetRemoteResolver(resolver)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	first := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	afterCatalog := resolver.calls.Load()
	if afterCatalog == 0 {
		t.Fatal("catalog snapshot never resolved providers")
	}
	overlay := getJSONPath(t, ts, "/api/v1/me/app-connections", http.StatusOK, "")
	if resolver.calls.Load() <= afterCatalog {
		t.Fatal("overlay reused a closed snapshot provider instead of resolving a live one")
	}
	var statuses []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(overlay, &statuses); err != nil {
		t.Fatalf("decode overlay: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Name != "slack" || statuses[0].Status == "" {
		t.Fatalf("overlay = %s", overlay)
	}
	second := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if string(first) != string(second) {
		t.Fatalf("cached catalog changed: %s vs %s", first, second)
	}
}

func TestAppCatalogRefreshesAfterProviderGenerationBump(t *testing.T) {
	t.Parallel()

	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack", ConnMode: core.ConnectionModeNone})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = testPluginDefsForConnections("slack", "default")
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	first := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var firstApps []appCatalogName
	if err := json.Unmarshal(first, &firstApps); err != nil {
		t.Fatalf("decode first catalog: %v", err)
	}
	if len(firstApps) != 1 || firstApps[0].DisplayName != "Slack" {
		t.Fatalf("first catalog = %s", first)
	}
	if err := providers.Replace("slack", &coretesting.StubIntegration{N: "slack", DN: "Slack Workspace", ConnMode: core.ConnectionModeNone}); err != nil {
		t.Fatalf("replace provider: %v", err)
	}
	second := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var secondApps []appCatalogName
	if err := json.Unmarshal(second, &secondApps); err != nil {
		t.Fatalf("decode second catalog: %v", err)
	}
	if len(secondApps) != 1 || secondApps[0].DisplayName != "Slack Workspace" {
		t.Fatalf("second catalog = %s, want Slack Workspace after generation bump", second)
	}
}

func TestAppCatalogRefreshesAfterPluginConnectionChange(t *testing.T) {
	t.Parallel()

	defs := testPluginDefsForConnections("slack", "default")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack", ConnMode: core.ConnectionModeNone})
		cfg.AppDefs = defs
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	first := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if connectionNamesFromCatalog(t, first, "slack")["workspace"] {
		t.Fatalf("first catalog already has workspace: %s", first)
	}
	defs["slack"].Connections["workspace"] = &config.ConnectionDef{
		ConnectionID: "slack:workspace",
		DisplayName:  "Workspace",
		Mode:         providermanifestv1.ConnectionModeSubject,
		Auth: config.ConnectionAuthDef{
			Type: providermanifestv1.AuthTypeManual,
		},
	}
	second := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if !connectionNamesFromCatalog(t, second, "slack")["workspace"] {
		t.Fatalf("second catalog missing workspace after plugin edit: %s", second)
	}
}

func TestAppCatalogRefreshesAfterConnectionSchemaChange(t *testing.T) {
	t.Parallel()

	defs := testPluginDefsForConnections("slack", "default")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack", ConnMode: core.ConnectionModeNone})
		cfg.AppDefs = defs
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	first := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if got := connectionDisplayNameFromCatalog(t, first, "slack", "default"); got != "" && got != "default" {
		t.Fatalf("first catalog default displayName = %q: %s", got, first)
	}
	defs["slack"].Connections["default"].DisplayName = "Workspace"
	second := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	if got := connectionDisplayNameFromCatalog(t, second, "slack", "default"); got != "Workspace" {
		t.Fatalf("second catalog default displayName = %q, want Workspace after schema edit: %s", got, second)
	}
}

type oneShotFailResolver struct {
	failName string
	failed   atomic.Bool
}

func (r *oneShotFailResolver) ResolveProvider(_ context.Context, name string) (core.Provider, error) {
	if name == r.failName && r.failed.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("tunnel dial failed")
	}
	return nil, core.ErrNotFound
}

func TestAppCatalogDoesNotCacheFailedProviderResolve(t *testing.T) {
	t.Parallel()

	resolver := &oneShotFailResolver{failName: "slack"}
	providers := testutil.NewProviderRegistry(t,
		&coretesting.StubIntegration{N: "slack", DN: "Slack", ConnMode: core.ConnectionModeNone},
		&coretesting.StubIntegration{N: "email", DN: "Email", ConnMode: core.ConnectionModeNone},
	)
	providers.SetRemoteResolver(resolver)

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Providers = providers
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"slack": testPluginDefsForConnections("slack", "default")["slack"],
			"email": testPluginDefsForConnections("email", "default")["email"],
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	first := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var firstApps []appCatalogName
	if err := json.Unmarshal(first, &firstApps); err != nil {
		t.Fatalf("decode first catalog: %v", err)
	}
	if _, ok := catalogNameSet(firstApps)["slack"]; ok {
		t.Fatalf("first catalog included slack after resolve failure: %s", first)
	}
	if _, ok := catalogNameSet(firstApps)["email"]; !ok {
		t.Fatalf("first catalog missing email: %s", first)
	}

	second := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "")
	var secondApps []appCatalogName
	if err := json.Unmarshal(second, &secondApps); err != nil {
		t.Fatalf("decode second catalog: %v", err)
	}
	names := catalogNameSet(secondApps)
	if _, ok := names["slack"]; !ok {
		t.Fatalf("second catalog still missing slack after failed resolve was not cached: %s", second)
	}
	if _, ok := names["email"]; !ok {
		t.Fatalf("second catalog missing email: %s", second)
	}
}

func TestAppCatalogSharesTenantSnapshotAcrossViewers(t *testing.T) {
	t.Parallel()

	aliceID := principal.UserSubjectID(testCanonicalViewerUserID)
	bobID := principal.UserSubjectID(testCanonicalAdminUserID)
	resolver := &countingMissResolver{}
	providers := testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "slack", DN: "Slack", ConnMode: core.ConnectionModeNone})
	providers.SetRemoteResolver(resolver)

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>app</html>")
	authz := &serverTestAuthorizationProvider{
		relationships: subjectSetGrant(aliceID, "viewer", "app", "slack"),
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
			switch token {
			case "alice-token":
				return testIntrospectActive(aliceID, ""), nil
			case "bob-token":
				return testIntrospectActive(bobID, ""), nil
			default:
				return &core.IntrospectResponse{Active: false}, nil
			}
		})
		cfg.Authorization = authz
		cfg.Providers = providers
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"slack": {
				Static:             &config.AppStaticConfig{Mount: "/slack"},
				ResolvedStaticRoot: rootDir,
			},
		}
		cfg.Services = testutil.NewStubServices(t)
	})
	testutil.CloseOnCleanup(t, ts)

	alice := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "Bearer alice-token")
	afterAlice := resolver.calls.Load()
	if afterAlice == 0 {
		t.Fatal("alice catalog never resolved providers")
	}
	var aliceApps []listedIntegration
	if err := json.Unmarshal(alice, &aliceApps); err != nil {
		t.Fatalf("decode alice catalog: %v", err)
	}
	if mounted, ok := mountedPathFor(aliceApps, "slack"); !ok || mounted == "" {
		t.Fatalf("alice catalog missing granted app: %s", alice)
	}

	bob := getJSONPath(t, ts, "/api/v1/catalog/apps", http.StatusOK, "Bearer bob-token")
	if resolver.calls.Load() != afterAlice {
		t.Fatalf("bob catalog rebuilt the tenant directory: resolver calls %d after alice, %d after bob", afterAlice, resolver.calls.Load())
	}
	var bobApps []listedIntegration
	if err := json.Unmarshal(bob, &bobApps); err != nil {
		t.Fatalf("decode bob catalog: %v", err)
	}
	if _, ok := mountedPathFor(bobApps, "slack"); ok {
		t.Fatalf("bob catalog advertised an app they cannot use: %s", bob)
	}
}

type appCatalogName struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

func connectionNamesFromCatalog(t *testing.T, body []byte, app string) map[string]bool {
	t.Helper()
	var apps []struct {
		Name        string `json:"name"`
		Connections []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &apps); err != nil {
		t.Fatalf("decode catalog connections: %v", err)
	}
	out := map[string]bool{}
	for _, entry := range apps {
		if entry.Name != app {
			continue
		}
		for _, connection := range entry.Connections {
			out[connection.Name] = true
		}
	}
	return out
}

func connectionDisplayNameFromCatalog(t *testing.T, body []byte, app, connection string) string {
	t.Helper()
	var apps []struct {
		Name        string `json:"name"`
		Connections []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &apps); err != nil {
		t.Fatalf("decode catalog connections: %v", err)
	}
	for _, entry := range apps {
		if entry.Name != app {
			continue
		}
		for _, conn := range entry.Connections {
			if conn.Name == connection {
				return conn.DisplayName
			}
		}
	}
	return ""
}

func catalogNameSet(apps []appCatalogName) map[string]struct{} {
	out := make(map[string]struct{}, len(apps))
	for _, app := range apps {
		out[app.Name] = struct{}{}
	}
	return out
}

func getJSONPath(t *testing.T, ts *httptest.Server, path string, wantStatus int, authorization string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", path, err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s status = %d, want %d: %s", path, resp.StatusCode, wantStatus, body)
	}
	return body
}

func jsonContainsError(body []byte, want string) bool {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Error == want
}
