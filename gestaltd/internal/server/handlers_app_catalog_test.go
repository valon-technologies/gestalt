package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
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
	for _, connection := range statuses[0].Connections {
		if connection.Connected {
			t.Fatalf("no-auth overlay connection %q must not be product-connected: %s", connection.Name, overlay)
		}
	}

	composed := getJSONPath(t, ts, "/api/v1/apps", http.StatusOK, "")
	var integrations []struct {
		Name        string `json:"name"`
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
	for i, connection := range integrations[0].Connections {
		if connection.Connected {
			t.Fatalf("composed no-auth connection[%d] must not be product-connected: %s", i, composed)
		}
	}
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
