package server_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestAppAdminMembersForbiddenForAuthorizationAdminOnly(t *testing.T) {
	t.Parallel()

	globalAdminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(globalAdminID, "admin", "authorization", "authorization"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("global-admin-token", globalAdminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer global-admin-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d, want 403: %s", response.StatusCode, body)
	}
}

func TestAppAdminAllowedOperationsForbiddenForAuthorizationAdminOnly(t *testing.T) {
	t.Parallel()

	globalAdminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(globalAdminID, "admin", "authorization", "authorization"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("global-admin-token", globalAdminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(
		http.MethodPut,
		ts.URL+"/api/v1/apps/g-issues/admin/allowed-operations",
		bytes.NewBufferString(`{"operations":{}}`),
	)
	request.Header.Set("Authorization", "Bearer global-admin-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT allowed-operations: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("PUT status = %d, want 403: %s", response.StatusCode, body)
	}
}
