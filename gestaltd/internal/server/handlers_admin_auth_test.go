package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAdminAPIAllowsUnauthenticatedAccessWhenAdminAuthorizationDisabled(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers")
	if err != nil {
		t.Fatalf("GET admin API: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
}

func TestAdminAPIRequiresSessionWhenAdminAuthorizationEnabled(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedAdminTestServer(t, true)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/runtime/providers")
	if err != nil {
		t.Fatalf("GET admin API: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 401: %s", resp.StatusCode, body)
	}
}

func TestAdminAPIAllowsAuthorizedAdminSession(t *testing.T) {
	t.Parallel()

	ts, authz := newAuthorizedAdminTestServerWithProvider(t, true)
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/runtime/providers", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin API: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}

	authz.mu.Lock()
	defer authz.mu.Unlock()
	if len(authz.checkAccessRequests) != 1 {
		t.Fatalf("CheckAccess calls = %d, want 1", len(authz.checkAccessRequests))
	}
	check := authz.checkAccessRequests[0]
	if got := check.GetAction().GetName(); got != "admin" {
		t.Fatalf("action = %q, want admin", got)
	}
	if got := check.GetResource(); got.GetType() != "gestalt" || got.GetId() != "gestalt" {
		t.Fatalf("resource = %s:%s, want gestalt:gestalt", got.GetType(), got.GetId())
	}
}

func TestAdminAPIRejectsAuthenticatedUserWithoutAdminRole(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedAdminTestServer(t, false)
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/v1/runtime/providers", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET admin API: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}
}

func newAuthorizedAdminTestServer(t *testing.T, grantAdmin bool) *httptest.Server {
	t.Helper()
	ts, _ := newAuthorizedAdminTestServerWithProvider(t, grantAdmin)
	return ts
}

func newAuthorizedAdminTestServerWithProvider(t *testing.T, grantAdmin bool) (*httptest.Server, *serverTestAuthorizationProvider) {
	t.Helper()

	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "admin-api-user", "admin-api-user@example.test", time.Now())

	relationships := []*proto.Relationship(nil)
	if grantAdmin {
		relationships = subjectSetGrant(principal.UserSubjectID(user.ID), "admin", "gestalt", "gestalt")
	}

	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{
			Name:    "gestalt",
			Actions: []*proto.ModelAction{{Name: "admin", Relations: []string{"admin"}}},
		}},
		relationships: relationships,
	}

	return newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = coretesting.NamedIntrospectIdentityStub("test", func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token != "session-token" {
				return nil, core.ErrNotFound
			}
			return &core.UserIdentity{Email: user.Email}, nil
		})
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.Admin = server.AdminRouteConfig{
			AuthorizationPolicy: "gestalt",
			AuthorizationAction: "admin",
		}
	}), authz
}
