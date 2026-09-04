package server_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

const evaluatorGroupID = "engineering"

// subjectSetGrant models the shape the mounted-UI and app-admin paths used to
// miss: the subject holds no direct grant on the resource, only membership in a
// group that in turn holds the role.
func subjectSetGrant(subjectID, role, resourceType, resourceID string) []*proto.Relationship {
	return []*proto.Relationship{
		{
			Tuple: &proto.RelationshipTuple{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
						Type: "subject",
						Id:   subjectID,
					}},
				},
				Relation: "member",
				Resource: &proto.Resource{Type: "group", Id: evaluatorGroupID},
			},
			SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
		},
		{
			Tuple: &proto.RelationshipTuple{
				Target: &proto.RelationshipTarget{
					Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{
						Resource: &proto.Resource{Type: "group", Id: evaluatorGroupID},
						Relation: "member",
					}},
				},
				Relation: role,
				Resource: &proto.Resource{Type: resourceType, Id: resourceID},
			},
			SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
		},
	}
}

func evaluatorAppDefs(appName string) map[string]*config.ProviderEntry {
	return map[string]*config.ProviderEntry{
		appName:    {},
		"g-issues": {},
	}
}

// appResourceTypeWithActions declares the shared "app" resource type with the
// given action names, which is what decides whether the evaluator can answer a
// mounted-UI question at all.
func appResourceTypeWithActions(actions ...string) []*proto.AuthorizationModelResourceType {
	resourceType := &proto.AuthorizationModelResourceType{Name: "app"}
	for _, action := range actions {
		resourceType.Actions = append(resourceType.Actions, &proto.ModelAction{Name: action})
	}
	return []*proto.AuthorizationModelResourceType{resourceType}
}

func evaluatorMountedUIServer(t *testing.T, authz *serverTestAuthorizationProvider, subjectID, appName string) *httptest.Server {
	t.Helper()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("ui-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = evaluatorAppDefs(appName)
		cfg.MountedUIs = []server.MountedUI{{
			Name:         "sample-ui",
			Path:         "/sample",
			AppName:      appName,
			AppLevelAuth: true,
			AllowedRoles: []string{"viewer"},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("sample-shell"))
			}),
		}}
	})
	testutil.CloseOnCleanup(t, ts)
	return ts
}

func evaluatorMountedUIStatus(t *testing.T, ts *httptest.Server) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sample/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer ui-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET mounted UI: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(body)
}

// TestMountedUIAllowsSubjectSetDerivedRole is the behavior change this stack is
// for: the subject has no direct relationship on the app, only a group that
// does. The old direct-only relationship scan denied it.
func TestMountedUIAllowsSubjectSetDerivedRole(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if body != "sample-shell" {
		t.Fatalf("body = %q, want sample-shell", body)
	}
}

// TestMountedUIStillGatesOnAllowedRoles proves the mount's AllowedRoles keep
// their meaning: a group-derived role outside the mount's set is denied.
func TestMountedUIStillGatesOnAllowedRoles(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: subjectSetGrant(subjectID, "editor", "app", "sampleApp"),
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", status, body)
	}
}

// TestMountedUIDeniesOnEvaluatorError keeps the boundary fail-closed.
func TestMountedUIDeniesOnEvaluatorError(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships:  subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
		checkAccessErr: errors.New("evaluator unavailable"),
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status == http.StatusOK {
		t.Fatalf("evaluator error was allowed through: %s", body)
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", status, body)
	}
}

func evaluatorAppAdminStatus(t *testing.T, authz *serverTestAuthorizationProvider, subjectID string) (int, string) {
	t.Helper()
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = evaluatorAppDefs("sampleApp")
	})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer admin-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, string(body)
}

// TestAppAdminAllowsSubjectSetDerivedAdmin covers the second bespoke path:
// group-derived admin now administers the app.
func TestAppAdminAllowsSubjectSetDerivedAdmin(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: subjectSetGrant(subjectID, "admin", "app", "g-issues"),
	}

	status, body := evaluatorAppAdminStatus(t, authz, subjectID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
}

// TestAppAdminDeniesSubjectSetDerivedNonAdmin keeps the admin-role requirement.
func TestAppAdminDeniesSubjectSetDerivedNonAdmin(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: subjectSetGrant(subjectID, "viewer", "app", "g-issues"),
	}

	status, body := evaluatorAppAdminStatus(t, authz, subjectID)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", status, body)
	}
}

// TestAppAdminDeniesOnEvaluatorError keeps the boundary fail-closed.
func TestAppAdminDeniesOnEvaluatorError(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships:  subjectSetGrant(subjectID, "admin", "app", "g-issues"),
		checkAccessErr: errors.New("evaluator unavailable"),
	}

	status, body := evaluatorAppAdminStatus(t, authz, subjectID)
	if status == http.StatusOK {
		t.Fatalf("evaluator error was allowed through: %s", body)
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", status, body)
	}
}

// TestMountedUIModelDeclaresActionTrustsEvaluator covers the normal case: the
// resource type declares an action matching the mount, so the evaluator's
// decision is authoritative and the server never scans relationships itself.
func TestMountedUIModelDeclaresActionTrustsEvaluator(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		resourceTypes: appResourceTypeWithActions("sampleApp"),
		relationships: subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if len(authz.listRelationshipRequests) != 0 {
		t.Fatalf("server-side relationship scan ran %d times, want 0", len(authz.listRelationshipRequests))
	}
}

// TestMountedUIModelWildcardActionTrustsEvaluator covers a resource type whose
// only action is the wildcard.
func TestMountedUIModelWildcardActionTrustsEvaluator(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		resourceTypes: appResourceTypeWithActions("*"),
		relationships: subjectSetGrant(subjectID, "viewer", "app", "sampleApp"),
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", status, body)
	}
	if len(authz.listRelationshipRequests) != 0 {
		t.Fatalf("server-side relationship scan ran %d times, want 0", len(authz.listRelationshipRequests))
	}
}

// TestMountedUIModelWithoutMatchingActionDeniesDirectGrant proves the
// evaluator remains the sole authority. A direct relationship cannot bypass
// its denial when the model does not define the requested action.
func TestMountedUIModelWithoutMatchingActionDeniesDirectGrant(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		resourceTypes: appResourceTypeWithActions("someOtherAction"),
		relationships: []*proto.Relationship{testAuthorizationRelationship(
			subjectID,
			"viewer",
			"app",
			"sampleApp",
		)},
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", status, body)
	}
	if len(authz.listRelationshipRequests) != 0 {
		t.Fatalf("server-side relationship scan ran %d times, want 0", len(authz.listRelationshipRequests))
	}
}

// TestMountedUITransportErrorFailsClosed keeps a provider failure fatal.
func TestMountedUITransportErrorFailsClosed(t *testing.T) {
	t.Parallel()

	subjectID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		resourceTypes: appResourceTypeWithActions("someOtherAction"),
		relationships: []*proto.Relationship{testAuthorizationRelationship(
			subjectID, "viewer", "app", "sampleApp",
		)},
		checkAccessErr: errors.New("evaluator unavailable"),
	}

	status, body := evaluatorMountedUIStatus(t, evaluatorMountedUIServer(t, authz, subjectID, "sampleApp"))
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", status, body)
	}
	if len(authz.listRelationshipRequests) != 0 {
		t.Fatalf("server-side relationship scan ran after transport error (%d calls)", len(authz.listRelationshipRequests))
	}
}
