package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestLookupUserByEmailRequiresAuthentication(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedUserLookupTestServer(t, userLookupTestGrants{gestaltAdmin: true})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/api/v1/users/lookup?email=admin-api-user%40example.test")
	if err != nil {
		t.Fatalf("GET user lookup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 401: %s", resp.StatusCode, body)
	}
}

func TestLookupUserByEmailRequiresAdminAccess(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedUserLookupTestServer(t, userLookupTestGrants{})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/users/lookup?email=admin-api-user%40example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET user lookup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403: %s", resp.StatusCode, body)
	}
}

func TestLookupUserByEmailAllowsAppAdmin(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedUserLookupTestServer(t, userLookupTestGrants{appAdmin: "traffic-cop"})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/users/lookup?email=admin-api-user%40example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET user lookup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
}

func TestLookupUserByEmailReturnsSubjectID(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedUserLookupTestServer(t, userLookupTestGrants{gestaltAdmin: true})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/users/lookup?email=admin-api-user%40example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET user lookup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}

	var payload struct {
		SubjectID string `json:"subjectId"`
		ID        string `json:"id"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.SubjectID != principal.UserSubjectID("admin-api-user") {
		t.Fatalf("subjectId = %q, want %q", payload.SubjectID, principal.UserSubjectID("admin-api-user"))
	}
	if payload.ID != "admin-api-user" {
		t.Fatalf("id = %q, want admin-api-user", payload.ID)
	}
	if payload.Email != "admin-api-user@example.test" {
		t.Fatalf("email = %q, want admin-api-user@example.test", payload.Email)
	}
}

func TestLookupUserByEmailNotFound(t *testing.T) {
	t.Parallel()

	ts := newAuthorizedUserLookupTestServer(t, userLookupTestGrants{gestaltAdmin: true})
	testutil.CloseOnCleanup(t, ts)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/users/lookup?email=missing%40example.test", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET user lookup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 404: %s", resp.StatusCode, body)
	}
}

func newAuthorizedUserLookupTestServer(t *testing.T, grants userLookupTestGrants) *httptest.Server {
	t.Helper()

	svc := testutil.NewStubServices(t)
	user := seedUserRecord(t, svc, "admin-api-user", "admin-api-user@example.test", time.Now())

	relationships := []*proto.Relationship(nil)
	if grants.gestaltAdmin {
		relationships = append(relationships, testAuthorizationRelationship(
			principal.UserSubjectID(user.ID),
			"admin",
			"gestaltAdmin",
			"gestaltAdmin",
		))
	}
	if grants.appAdmin != "" {
		relationships = append(relationships, testAuthorizationRelationship(
			principal.UserSubjectID(user.ID),
			"admin",
			"app",
			grants.appAdmin,
		))
	}

	authz := &serverTestAuthorizationProvider{
		resourceTypes: []*proto.AuthorizationModelResourceType{{
			Name: "gestaltAdmin",
		}},
		relationships: relationships,
	}

	return newTestServer(t, func(cfg *server.Config) {
		if grants.appAdmin != "" {
			cfg.AppDefs = map[string]*config.ProviderEntry{
				grants.appAdmin: {},
			}
		}
		cfg.Auth = coretesting.NamedIntrospectIdentityStub("test", func(_ context.Context, token string) (*core.UserIdentity, error) {
			if token != "session-token" {
				return nil, core.ErrNotFound
			}
			return &core.UserIdentity{Email: user.Email}, nil
		})
		cfg.Services = svc
		cfg.Authorization = authz
		cfg.AdminUI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("admin-shell"))
		})
	})
}

type userLookupTestGrants struct {
	gestaltAdmin bool
	appAdmin     string
}
