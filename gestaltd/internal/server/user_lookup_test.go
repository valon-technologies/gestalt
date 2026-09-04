package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type memberEmailRow struct {
	Email     string `json:"email"`
	Role      string `json:"role"`
	SubjectID string `json:"subjectId"`
}

// userLookupMembersRows fetches the app-admin member roster for an admin that
// holds app-scoped admin plus, optionally, the employee operator role.
func userLookupMembersRows(t *testing.T, withOperatorRole bool) []memberEmailRow {
	t.Helper()

	services := testutil.NewStubServices(t)
	member := seedUser(t, services, "bob@valon.com")
	memberSubject := principal.UserSubjectID(member.ID)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)

	relationships := []*proto.Relationship{
		testAuthorizationRelationship(adminSubject, "admin", "app", "g-issues"),
		testAuthorizationRelationship(memberSubject, "viewer", "app", "g-issues"),
	}
	if withOperatorRole {
		relationships = append(relationships,
			testAuthorizationRelationship(adminSubject, testUserLookupRole, testUserLookupResource, testUserLookupResource))
	}
	authz := &serverTestAuthorizationProvider{relationships: relationships}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", adminSubject, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET members status = %d: %s", response.StatusCode, body)
	}
	var rows []memberEmailRow
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	return rows
}

func memberEmailFor(rows []memberEmailRow, subjectID string) (string, bool) {
	for _, row := range rows {
		if row.SubjectID == subjectID {
			return row.Email, true
		}
	}
	return "", false
}

// TestAppAdminAloneCannotEnumerateUsers is the restriction the plan asks for:
// administering an app must not, on its own, turn the admin surface into a
// directory. The roster still lists the grants; only the identity lookup is
// withheld.
func TestAppAdminAloneCannotEnumerateUsers(t *testing.T) {
	t.Parallel()

	rows := userLookupMembersRows(t, false)
	if len(rows) != 2 {
		t.Fatalf("members = %#v, want 2 rows", rows)
	}
	for _, row := range rows {
		if row.Email != "" {
			t.Fatalf("app-scoped admin resolved an email without the operator role: %#v", row)
		}
	}
}

// TestEmployeeOperatorRoleAllowsUserLookup is the positive half: the explicit
// operator role, and only it, restores identity resolution.
func TestEmployeeOperatorRoleAllowsUserLookup(t *testing.T) {
	t.Parallel()

	rows := userLookupMembersRows(t, true)
	if len(rows) != 2 {
		t.Fatalf("members = %#v, want 2 rows", rows)
	}
	found := false
	for _, row := range rows {
		if row.Email == "bob@valon.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("operator role did not resolve member email: %#v", rows)
	}
}

// TestUserLookupHonorsGroupDerivedOperatorRole proves the gate goes through the
// shared evaluator, so the operator role may be held through a group.
func TestUserLookupHonorsGroupDerivedOperatorRole(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member := seedUser(t, services, "bob@valon.com")
	memberSubject := principal.UserSubjectID(member.ID)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)

	relationships := []*proto.Relationship{
		testAuthorizationRelationship(adminSubject, "admin", "app", "g-issues"),
		testAuthorizationRelationship(memberSubject, "viewer", "app", "g-issues"),
	}
	relationships = append(relationships,
		subjectSetGrant(adminSubject, testUserLookupRole, testUserLookupResource, testUserLookupResource)...)
	authz := &serverTestAuthorizationProvider{relationships: relationships}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", adminSubject, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	var rows []memberEmailRow
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	email, ok := memberEmailFor(rows, memberSubject)
	if !ok {
		t.Fatalf("member row missing: %#v", rows)
	}
	if email != "bob@valon.com" {
		t.Fatalf("group-derived operator role did not resolve email: %#v", rows)
	}
}

// TestUserLookupDeniesWhenModelDeclaresNoMatchingAction proves user lookup
// trusts the evaluator's decision and never treats a direct relationship as a
// second source of truth.
func TestUserLookupDeniesWhenModelDeclaresNoMatchingAction(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member := seedUser(t, services, "bob@valon.com")
	memberSubject := principal.UserSubjectID(member.ID)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)

	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminSubject, "admin", "app", "g-issues"),
			testAuthorizationRelationship(memberSubject, "viewer", "app", "g-issues"),
			testAuthorizationRelationship(adminSubject, testUserLookupRole, testUserLookupResource, testUserLookupResource),
		},
		// The app type answers actions; the user-lookup type deliberately does
		// not answer its requested action.
		resourceTypes: []*proto.AuthorizationModelResourceType{
			{Name: "app", Actions: []*proto.ModelAction{{Name: "*"}}},
			{Name: testUserLookupResource},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", adminSubject, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET members status = %d: %s", response.StatusCode, body)
	}
	var rows []memberEmailRow
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode members: %v", err)
	}

	email, ok := memberEmailFor(rows, memberSubject)
	if !ok {
		t.Fatalf("member row missing: %#v", rows)
	}
	if email != "" {
		t.Fatalf("direct operator relationship bypassed the evaluator denial: %#v", rows)
	}
}
