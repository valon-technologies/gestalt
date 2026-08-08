package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type listedIntegration struct {
	Name        string `json:"name"`
	MountedPath string `json:"mountedPath"`
}

// listingTestServer mounts two static app UIs behind app-level authorization so
// /apps has two mounted-UI decisions to make in one request.
func listingTestServer(t *testing.T, authz *serverTestAuthorizationProvider, subjectID string) *httptest.Server {
	t.Helper()

	rootDir := t.TempDir()
	writeTestUIAsset(t, filepath.Join(rootDir, "index.html"), "<html>app</html>")

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("listing-token", subjectID, "")
		cfg.Authorization = authz
		cfg.Services = testutil.NewStubServices(t)
		cfg.Providers = testutil.NewProviderRegistry(t,
			&coretesting.StubIntegration{N: "sampleApp", DN: "Sample", ConnMode: core.ConnectionModeNone},
			&coretesting.StubIntegration{N: "otherApp", DN: "Other", ConnMode: core.ConnectionModeNone},
		)
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"sampleApp": {
				Static:             &config.AppStaticConfig{Mount: "/sample"},
				ResolvedStaticRoot: rootDir,
			},
			"otherApp": {
				Static:             &config.AppStaticConfig{Mount: "/other"},
				ResolvedStaticRoot: rootDir,
			},
		}
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func listIntegrationsForTest(t *testing.T, ts *httptest.Server) []listedIntegration {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer listing-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET apps: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET apps status = %d: %s", resp.StatusCode, body)
	}
	var integrations []listedIntegration
	if err := json.NewDecoder(resp.Body).Decode(&integrations); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	return integrations
}

func mountedPathFor(integrations []listedIntegration, name string) (string, bool) {
	for _, integration := range integrations {
		if integration.Name == name {
			return integration.MountedPath, true
		}
	}
	return "", false
}

// TestAppsListingBatchesGroupDerivedDecisions is the core listing guarantee: a
// subject whose only grant comes from a group still sees its app, and the whole
// listing costs one batched evaluator call instead of one per app.
func TestAppsListingBatchesGroupDerivedDecisions(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: append(
			subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
			subjectSetGrant(subjectID, "viewer", "app", "otherApp")...,
		),
	}

	integrations := listIntegrationsForTest(t, listingTestServer(t, authz, subjectID))
	for _, name := range []string{"sampleApp", "otherApp"} {
		mountedPath, ok := mountedPathFor(integrations, name)
		if !ok {
			t.Fatalf("app %q missing from listing: %#v", name, integrations)
		}
		if mountedPath == "" {
			t.Fatalf("group-granted app %q lost its mounted path", name)
		}
	}
	if len(authz.checkAccessManyRequests) != 1 {
		t.Fatalf("CheckAccessMany calls = %d, want exactly 1 batched call", len(authz.checkAccessManyRequests))
	}
	if got := len(authz.checkAccessRequests); got != 0 {
		t.Fatalf("per-item CheckAccess calls = %d, want 0 when the batch answers everything", got)
	}
}

// TestAppsListingHidesUngrantedApp proves the filtering is real: a subject with
// no grant does not see the app through an otherwise usable settings surface.
func TestAppsListingHidesUngrantedApp(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
	}

	integrations := listIntegrationsForTest(t, listingTestServer(t, authz, subjectID))
	if mountedPath, _ := mountedPathFor(integrations, "sampleApp"); mountedPath == "" {
		t.Fatalf("granted app lost its mounted path: %#v", integrations)
	}
	if _, ok := mountedPathFor(integrations, "otherApp"); ok {
		t.Fatalf("ungranted app exposed in listing: %#v", integrations)
	}
}

// TestAppsListingHonorsDefaultRole is the access-loss regression guard for
// Gate A. The subject holds no relationship at all and is visible only through
// the resource type's defaultRole. Batched listing must reach the same answer
// the mounted-UI boundary reaches, or every employee silently loses their apps
// the moment listing is filtered.
func TestAppsListingHonorsDefaultRole(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{
			Name:        "app",
			DefaultRole: "viewer",
			Actions:     []*proto.ModelAction{{Name: "*"}},
		}},
	}

	integrations := listIntegrationsForTest(t, listingTestServer(t, authz, subjectID))
	for _, name := range []string{"sampleApp", "otherApp"} {
		mountedPath, ok := mountedPathFor(integrations, name)
		if !ok {
			t.Fatalf("defaultRole app %q missing from listing: %#v", name, integrations)
		}
		if mountedPath == "" {
			t.Fatalf("defaultRole app %q lost its mounted path", name)
		}
	}
	if len(authz.checkAccessManyRequests) != 1 {
		t.Fatalf("CheckAccessMany calls = %d, want exactly 1 batched call", len(authz.checkAccessManyRequests))
	}
}

// TestAppsListingFallsBackWhenBatchFails proves the batch is an optimization,
// never a gate: a provider that cannot serve CheckAccessMany must not cost the
// caller a single app.
func TestAppsListingFallsBackWhenBatchFails(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships:      subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
		checkAccessManyErr: errors.New("batch rpc unimplemented"),
	}

	integrations := listIntegrationsForTest(t, listingTestServer(t, authz, subjectID))
	if mountedPath, _ := mountedPathFor(integrations, "sampleApp"); mountedPath == "" {
		t.Fatalf("granted app hidden after batch failure: %#v", integrations)
	}
	if len(authz.checkAccessRequests) == 0 {
		t.Fatal("batch failure did not fall back to per-item decisions")
	}
}

func TestAppsListingFallsThroughDeniedSettingsToAuthorizedOperations(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	checker := &allowOneOperationAccess{allowed: "items.list"}
	stub := &coretesting.StubIntegration{
		N:        "sampleApp",
		DN:       "Sample",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "sampleApp",
			Operations: []catalog.CatalogOperation{
				{ID: "items.list", Method: http.MethodGet, Path: "/items"},
			},
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("listing-token", subjectID, "")
		cfg.Authorization = &serverTestAuthorizationProvider{}
		cfg.Services = testutil.NewStubServices(t)
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.AppDefs = map[string]*config.ProviderEntry{"sampleApp": {}}
		cfg.OperationAccessChecker = checker
	})
	testutil.CloseOnCleanup(t, ts)

	integrations := listIntegrationsForTest(t, ts)
	if _, ok := mountedPathFor(integrations, "sampleApp"); !ok {
		t.Fatalf("operation-authorized app missing from listing: %#v", integrations)
	}
	if checker.calls != 1 || checker.queries != 1 {
		t.Fatalf("operation checks = {%d calls, %d queries}, want {1, 1}", checker.calls, checker.queries)
	}
}

// erroringOperationAccess is an evaluator that cannot answer, used to prove a
// listing surfaces the failure rather than returning an empty list.
type erroringOperationAccess struct{}

func (erroringOperationAccess) CheckOperationAccessMany(
	context.Context, *principal.Principal, []invocation.OperationAccessQuery,
) ([]error, error) {
	return nil, errors.New("evaluator unavailable")
}

// allowOneOperationAccess allows exactly one operation and counts its calls, so
// a test can prove operation listing asks once for the whole catalog.
type allowOneOperationAccess struct {
	allowed string
	calls   int
	queries int
}

func (a *allowOneOperationAccess) CheckOperationAccessMany(
	_ context.Context, _ *principal.Principal, queries []invocation.OperationAccessQuery,
) ([]error, error) {
	a.calls++
	a.queries += len(queries)
	results := make([]error, len(queries))
	for i, query := range queries {
		if query.Operation != a.allowed {
			results[i] = errors.New("denied")
		}
	}
	return results, nil
}

func operationsListingServer(t *testing.T, checker invocation.OperationAccessChecker) *httptest.Server {
	t.Helper()
	stub := &coretesting.StubIntegration{
		N:        "sampleApp",
		DN:       "Sample",
		ConnMode: core.ConnectionModeNone,
		CatalogVal: &catalog.Catalog{
			Name: "sampleApp",
			Operations: []catalog.CatalogOperation{
				{ID: "items.list", Method: "GET", Path: "/items"},
				{ID: "items.create", Method: "POST", Path: "/items"},
			},
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Services = testutil.NewStubServices(t)
		cfg.Providers = testutil.NewProviderRegistry(t, stub)
		cfg.AppDefs = map[string]*config.ProviderEntry{"sampleApp": {}}
		cfg.OperationAccessChecker = checker
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func getOperations(t *testing.T, ts *httptest.Server) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/sampleApp/operations", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET operations: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, body
}

// TestOperationListingFiltersWithOneBatchedCall pins both properties at once:
// the catalog is filtered to what the caller may invoke, and the whole catalog
// is decided in a single batched question.
func TestOperationListingFiltersWithOneBatchedCall(t *testing.T) {
	t.Parallel()

	checker := &allowOneOperationAccess{allowed: "items.list"}
	status, body := getOperations(t, operationsListingServer(t, checker))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	var ops []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &ops); err != nil {
		t.Fatalf("decode operations: %v", err)
	}
	if len(ops) != 1 || ops[0].ID != "items.list" {
		t.Fatalf("operations = %#v, want only items.list", ops)
	}
	if checker.calls != 1 {
		t.Fatalf("batched calls = %d, want 1", checker.calls)
	}
	if checker.queries != 2 {
		t.Fatalf("batched queries = %d, want 2 (one per operation)", checker.queries)
	}
}

// TestOperationListingFailsLoudlyWhenEvaluatorUnavailable is the "never a
// silently empty list" guard.
func TestOperationListingFailsLoudlyWhenEvaluatorUnavailable(t *testing.T) {
	t.Parallel()

	status, body := getOperations(t, operationsListingServer(t, erroringOperationAccess{}))
	if status == http.StatusOK {
		t.Fatalf("evaluator failure returned 200 with body %s", body)
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", status, body)
	}
}
