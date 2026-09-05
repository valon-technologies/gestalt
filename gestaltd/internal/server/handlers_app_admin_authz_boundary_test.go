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

func TestAppAdminSurfacesForbiddenForAuthorizationAdminOnly(t *testing.T) {
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

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "members",
			method: http.MethodGet,
			path:   "/api/v1/apps/g-issues/admin/members",
		},
		{
			name:   "allowed_operations",
			method: http.MethodPut,
			path:   "/api/v1/apps/g-issues/admin/allowed-operations",
			body:   `{"operations":{}}`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var body io.Reader
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			}
			request, _ := http.NewRequest(tc.method, ts.URL+tc.path, body)
			request.Header.Set("Authorization", "Bearer global-admin-token")
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusForbidden {
				respBody, _ := io.ReadAll(response.Body)
				t.Fatalf("status = %d, want 403: %s", response.StatusCode, respBody)
			}
		})
	}
}
