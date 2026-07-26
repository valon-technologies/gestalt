package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAppAdminRegistrySelectAndRead(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	subjectID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = fixture.Reader
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/version", bytes.NewBufferString(`{"version":"`+fixture.Version+`"}`))
	request.Header.Set("Authorization", "Bearer alice-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST registry version: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST status = %d: %s", response.StatusCode, body)
	}
	var selected struct {
		FromVersion    string `json:"fromVersion"`
		DesiredVersion string `json:"desiredVersion"`
		Rollout        struct {
			State string `json:"state"`
		} `json:"rollout"`
	}
	if err := json.NewDecoder(response.Body).Decode(&selected); err != nil {
		t.Fatalf("decode POST response: %v", err)
	}
	if selected.FromVersion != "registry:first-install" || selected.DesiredVersion != fixture.Version || selected.Rollout.State != "enrolling" {
		t.Fatalf("selection response = %#v", selected)
	}

	request, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET registry state: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}
	var state struct {
		DesiredVersion    string `json:"desiredVersion"`
		SelectionDisabled bool   `json:"selectionDisabled"`
		DisabledReason    string `json:"disabledReason"`
		PublishedVersions []struct {
			SourceRef   string `json:"sourceRef"`
			SourceURL   string `json:"sourceUrl"`
			Publication struct {
				WorkflowRunURL string `json:"workflowRunUrl"`
			} `json:"publication"`
		} `json:"publishedVersions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if state.DesiredVersion != fixture.Version || !state.SelectionDisabled || state.DisabledReason != "rollout in progress" {
		t.Fatalf("registry state = %#v", state)
	}
	if len(state.PublishedVersions) != 1 || state.PublishedVersions[0].SourceRef == "" ||
		state.PublishedVersions[0].SourceURL == "" || state.PublishedVersions[0].Publication.WorkflowRunURL == "" {
		t.Fatalf("published versions = %#v", state.PublishedVersions)
	}
}

func TestAppAdminRegistryFailsClosed(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	subjectID := principal.UserSubjectID("alice")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = fixture.Reader
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET registry state: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 503: %s", response.StatusCode, body)
	}
}

func TestListAppsIncludesManagementPathForUninstalledRegistryApp(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET apps: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	var apps []struct {
		Name           string `json:"name"`
		ManagementPath string `json:"managementPath"`
	}
	if err := json.NewDecoder(response.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "g-issues" || apps[0].ManagementPath != "/apps/g-issues/admin" {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestListAppsIncludesManagementPathForAppAdmin(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.Providers = testutil.NewProviderRegistry(t, &coretesting.StubIntegration{N: "g-issues", ConnMode: core.ConnectionModeNone})
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET apps: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	var apps []struct {
		Name           string `json:"name"`
		ManagementPath string `json:"managementPath"`
	}
	if err := json.NewDecoder(response.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "g-issues" || apps[0].ManagementPath != "/apps/g-issues/admin" {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestAppAdminRegistryIncludesPendingAndFailedVersions(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	subjectID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	reader := registrytest.NewReaderWithCatalogs(t, fixture, registrytest.CatalogDocuments{
		PendingJSON: []byte(`{
  "schemaVersion": 1,
  "app": "g-issues",
  "pending": {
    "0.0.0-snapshot.gpending": {
      "version": "0.0.0-snapshot.gpending",
      "sourceRef": "abc123def456abc123def456abc123def456abcd",
      "repository": "github.com/valon-technologies/valon-tools",
      "startedAt": "2026-07-24T19:00:00Z",
      "updatedAt": "2026-07-24T19:04:12Z",
      "phase": "publishing"
    },
    "` + fixture.Version + `": {
      "version": "` + fixture.Version + `",
      "sourceRef": "abc123def456abc123def456abc123def456abcd",
      "startedAt": "2026-07-24T19:00:00Z",
      "updatedAt": "2026-07-24T19:04:12Z",
      "phase": "publishing"
    }
  }
}`),
		FailedJSON: []byte(`{
  "schemaVersion": 1,
  "app": "g-issues",
  "failed": {
    "0.0.0-snapshot.gfailed": {
      "version": "0.0.0-snapshot.gfailed",
      "sourceRef": "abc123def456abc123def456abc123def456abcd",
      "repository": "github.com/valon-technologies/valon-tools",
      "startedAt": "2026-07-24T18:00:00Z",
      "failedAt": "2026-07-24T18:35:00Z",
      "reason": "stale"
    }
  }
}`),
	})
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = reader
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET registry state: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}
	var state struct {
		PublishedVersions []struct {
			Version string `json:"version"`
		} `json:"publishedVersions"`
		PendingVersions []struct {
			Version              string `json:"version"`
			PublishingForSeconds *int64 `json:"publishingForSeconds"`
		} `json:"pendingVersions"`
		FailedVersions []struct {
			Version                string `json:"version"`
			Reason                 string `json:"reason"`
			PublishDurationSeconds *int64 `json:"publishDurationSeconds"`
		} `json:"failedVersions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if len(state.PublishedVersions) != 1 || state.PublishedVersions[0].Version != fixture.Version {
		t.Fatalf("publishedVersions = %#v", state.PublishedVersions)
	}
	if len(state.PendingVersions) != 1 || state.PendingVersions[0].Version != "0.0.0-snapshot.gpending" {
		t.Fatalf("pendingVersions = %#v", state.PendingVersions)
	}
	if state.PendingVersions[0].PublishingForSeconds == nil {
		t.Fatalf("pendingVersions missing publishingForSeconds: %#v", state.PendingVersions)
	}
	if len(state.FailedVersions) != 1 || state.FailedVersions[0].Version != "0.0.0-snapshot.gfailed" {
		t.Fatalf("failedVersions = %#v", state.FailedVersions)
	}
	if state.FailedVersions[0].PublishDurationSeconds == nil || state.FailedVersions[0].Reason != "stale" {
		t.Fatalf("failedVersions = %#v", state.FailedVersions)
	}
}

func TestAppAdminRegistryHistory(t *testing.T) {
	t.Parallel()

	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	subjectID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = fixture.Reader
	})
	testutil.CloseOnCleanup(t, ts)

	firstAt := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	secondAt := time.Date(2026, 7, 24, 16, 42, 0, 0, time.UTC)
	thirdAt := time.Date(2026, 7, 25, 9, 10, 0, 0, time.UTC)
	otherVersion := "0.0.0-snapshot.gdef456"

	appendHistoryRequest := func(fromVersion, toVersion string, at time.Time) string {
		t.Helper()
		request, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
			App:         "g-issues",
			FromVersion: fromVersion,
			ToVersion:   toVersion,
			Actor:       "user:alice",
			Timestamp:   at,
			Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
				AppName:   "g-issues",
				Version:   toVersion,
				SourceRef: "abc123def456abc123def456abc123def456abcd",
				Registry:  "toolshed",
			}),
		})
		if err != nil {
			t.Fatalf("AppendRequest: %v", err)
		}
		return request.ID
	}

	firstID := appendHistoryRequest(appregistry.FirstInstallFromVersion, fixture.Version, firstAt)
	appendHistoryRequest(fixture.Version, otherVersion, secondAt)
	thirdID := appendHistoryRequest(otherVersion, fixture.Version, thirdAt)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry/history", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}
	var history struct {
		Revisions []struct {
			ID              string `json:"id"`
			Version         string `json:"version"`
			PreviousVersion string `json:"previousVersion"`
			DeployedBy      string `json:"deployedBy"`
			DeploymentState string `json:"deploymentState"`
			Current         bool   `json:"current"`
			Publication     struct {
				WorkflowRunURL string `json:"workflowRunUrl"`
			} `json:"publication"`
		} `json:"revisions"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history.Revisions) != 3 {
		t.Fatalf("revisions = %#v", history.Revisions)
	}
	if history.Revisions[0].ID != thirdID || history.Revisions[0].Version != fixture.Version ||
		history.Revisions[0].PreviousVersion != otherVersion || !history.Revisions[0].Current ||
		history.Revisions[0].DeploymentState != "desired" || history.Revisions[0].DeployedBy != "user:alice" {
		t.Fatalf("newest revision = %#v", history.Revisions[0])
	}
	if history.Revisions[2].ID != firstID || history.Revisions[2].PreviousVersion != "" {
		t.Fatalf("oldest revision = %#v", history.Revisions[2])
	}
	if history.Revisions[0].Publication.WorkflowRunURL == "" {
		t.Fatalf("publication missing on newest revision: %#v", history.Revisions[0])
	}

	request, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry/history?limit=1", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET history page 1: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET page 1 status = %d: %s", response.StatusCode, body)
	}
	var page1 struct {
		Revisions []struct {
			ID string `json:"id"`
		} `json:"revisions"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page 1: %v", err)
	}
	if len(page1.Revisions) != 1 || page1.Revisions[0].ID != thirdID || page1.NextCursor == "" {
		t.Fatalf("page1 = %#v", page1)
	}

	request, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry/history?limit=1&cursor="+page1.NextCursor, nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET history page 2: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET page 2 status = %d: %s", response.StatusCode, body)
	}
	var page2 struct {
		Revisions []struct {
			ID string `json:"id"`
		} `json:"revisions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	if len(page2.Revisions) != 1 || page2.Revisions[0].ID == thirdID {
		t.Fatalf("page2 = %#v", page2)
	}
}
