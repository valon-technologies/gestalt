package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type memberIdentityRow struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	SubjectID   string `json:"subjectId"`
}

func memberIdentityFor(t *testing.T, rows []memberIdentityRow, subjectID string) memberIdentityRow {
	t.Helper()
	for _, row := range rows {
		if row.SubjectID == subjectID {
			return row
		}
	}
	t.Fatalf("member %s missing: %#v", subjectID, rows)
	return memberIdentityRow{}
}

// TestRosterAdminSeesRosterMemberIdentity: an admin who does not hold the
// employee operator role still sees the name and email of the people on the
// roster they administer, so the roster never falls back to raw subject IDs.
func TestRosterAdminSeesRosterMemberIdentity(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member, err := services.Users.FindOrCreateUserWithName(context.Background(), "bob@valon.com", "Bob Builder")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	memberSubject := principal.UserSubjectID(member.ID)
	memberEmailSubject := principal.UserSubjectID(member.Email)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)
	listerSubject := principal.UserSubjectID(testCanonicalViewerUserID)
	const groupID = "staff-group"

	authz := &serverTestAuthorizationProvider{relationships: []*proto.Relationship{
		testAuthorizationRelationship(adminSubject, "admin", "app", "g-issues"),
		testAuthorizationRelationship(memberSubject, "viewer", "app", "g-issues"),
		testAuthorizationRelationship(memberEmailSubject, "viewer", "app", "g-issues"),
		testAuthorizationRelationship(adminSubject, "admin", "group", groupID),
		testAuthorizationRelationship(memberSubject, "member", "group", groupID),
		testAuthorizationRelationship(memberEmailSubject, "member", "group", groupID),
		testAuthorizationRelationship(listerSubject, "viewer", "authorization", "authorization"),
	}}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = testAuthStubWithIntrospect(func(_ context.Context, token string) (*core.IntrospectResponse, error) {
			switch token {
			case "admin-token":
				return testIntrospectActive(adminSubject, ""), nil
			case "lister-token":
				return testIntrospectActive(listerSubject, ""), nil
			default:
				return &core.IntrospectResponse{Active: false}, nil
			}
		})
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	for name, path := range map[string]string{
		"app members":   "/api/v1/apps/g-issues/admin/members",
		"group members": "/api/v1/groups/" + groupID + "/admin/members",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
			request.Header.Set("Authorization", "Bearer admin-token")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("GET %s status = %d: %s", path, response.StatusCode, body)
			}
			var rows []memberIdentityRow
			if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
				t.Fatalf("decode members: %v", err)
			}
			got := memberIdentityFor(t, rows, memberSubject)
			if got.Email != "bob@valon.com" || got.DisplayName != "Bob Builder" {
				t.Fatalf("member identity = %#v, want bob@valon.com / Bob Builder", got)
			}
			// An email subject already carries its email; resolving it through the
			// directory (for a name) is user lookup, reserved for operators.
			got = memberIdentityFor(t, rows, memberEmailSubject)
			if got.Email != "bob@valon.com" || got.DisplayName != "" {
				t.Fatalf("email-subject identity = %#v, want bob@valon.com with no name", got)
			}
		})
	}

	t.Run("group lister sees roster identity", func(t *testing.T) {
		t.Parallel()
		request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups/"+groupID+"/admin/members", nil)
		request.Header.Set("Authorization", "Bearer lister-token")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("GET group members as lister: %v", err)
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("GET group members as lister status = %d: %s", response.StatusCode, body)
		}
		var rows []memberIdentityRow
		if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
			t.Fatalf("decode group members as lister: %v", err)
		}
		got := memberIdentityFor(t, rows, memberSubject)
		if got.Email != "bob@valon.com" || got.DisplayName != "Bob Builder" {
			t.Fatalf("group lister identity = %#v, want bob@valon.com / Bob Builder", got)
		}
	})
}

// registryDeployedBy returns how registry history labels a user who deployed
// an app. That label is directory lookup: it resolves a subject ID the caller
// has no roster relationship with, so it stays gated on the operator role.
func registryDeployedBy(
	t *testing.T,
	extra []*proto.Relationship,
	resourceTypes []*proto.AuthorizationModelResourceType,
) (label, deployerID string) {
	t.Helper()

	fixture := registrytest.NewInstallFixture(t)
	services := testutil.NewStubServices(t)
	deployer := seedUser(t, services, "bob@valon.com")
	deployerSubject := principal.UserSubjectID(deployer.ID)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)

	relationships := append([]*proto.Relationship{
		testAuthorizationRelationship(adminSubject, "admin", "app", "g-issues"),
	}, extra...)
	authz := &serverTestAuthorizationProvider{relationships: relationships, resourceTypes: resourceTypes}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", adminSubject, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": fixture.Registry}
		cfg.AppRegistryReader = fixture.Reader
	})
	testutil.CloseOnCleanup(t, ts)

	if _, err := services.AppVersionChangeRequests.AppendRequest(context.Background(), &core.AppVersionChangeRequest{
		App:         "g-issues",
		FromVersion: appregistry.FirstInstallFromVersion,
		ToVersion:   fixture.Version,
		Actor:       deployerSubject,
		Timestamp:   time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		Metadata: coredata.ChangeRequestMetadata(&core.AppInstallation{
			AppName:   "g-issues",
			Version:   fixture.Version,
			SourceRef: "abc123def456abc123def456abc123def456abcd",
			Registry:  "toolshed",
		}),
	}); err != nil {
		t.Fatalf("AppendRequest: %v", err)
	}

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry/history", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET history status = %d: %s", response.StatusCode, body)
	}
	var history struct {
		Revisions []struct {
			DeployedBy string `json:"deployedBy"`
		} `json:"revisions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history.Revisions) != 1 {
		t.Fatalf("revisions = %#v, want 1", history.Revisions)
	}
	return history.Revisions[0].DeployedBy, deployer.ID
}

func operatorGrant() *proto.Relationship {
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)
	return testAuthorizationRelationship(adminSubject, testUserLookupRole, testUserLookupResource, testUserLookupResource)
}

// TestAppAdminAloneCannotEnumerateUsers: administering an app must not, on its
// own, resolve people the admin has no roster relationship with. Without the
// operator role the label is the subject ID as-is.
func TestAppAdminAloneCannotEnumerateUsers(t *testing.T) {
	t.Parallel()

	got, deployerID := registryDeployedBy(t, nil, nil)
	if got != deployerID {
		t.Fatalf("app-scoped admin label = %q, want the unresolved subject ID %q", got, deployerID)
	}
}

func TestEmployeeOperatorRoleAllowsUserLookup(t *testing.T) {
	t.Parallel()

	if got, _ := registryDeployedBy(t, []*proto.Relationship{operatorGrant()}, nil); got != "bob@valon.com" {
		t.Fatalf("operator role did not resolve email: %q", got)
	}
}

// TestUserLookupHonorsGroupDerivedOperatorRole proves the gate goes through the
// shared evaluator, so the operator role may be held through a group.
func TestUserLookupHonorsGroupDerivedOperatorRole(t *testing.T) {
	t.Parallel()

	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)
	extra := subjectSetGrant(adminSubject, testUserLookupRole, testUserLookupResource, testUserLookupResource)
	if got, _ := registryDeployedBy(t, extra, nil); got != "bob@valon.com" {
		t.Fatalf("group-derived operator role did not resolve email: %q", got)
	}
}

// TestUserLookupDeniesWhenModelDeclaresNoMatchingAction proves user lookup
// trusts the evaluator's decision and never treats a direct relationship as a
// second source of truth.
func TestUserLookupDeniesWhenModelDeclaresNoMatchingAction(t *testing.T) {
	t.Parallel()

	// The app type answers actions; the user-lookup type deliberately does not
	// answer its requested action.
	resourceTypes := []*proto.AuthorizationModelResourceType{
		{Name: "app", Actions: []*proto.ModelAction{{Name: "*"}}},
		{Name: testUserLookupResource},
	}
	got, deployerID := registryDeployedBy(t, []*proto.Relationship{operatorGrant()}, resourceTypes)
	if got != deployerID {
		t.Fatalf("direct operator relationship bypassed the evaluator denial: label %q, want %q", got, deployerID)
	}
}

func getMembers(t *testing.T, url, token string) []memberIdentityRow {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status = %d: %s", url, response.StatusCode, body)
	}
	var payload json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	var rows []memberIdentityRow
	if err := json.Unmarshal(payload, &rows); err != nil {
		var wrapped struct {
			Members []memberIdentityRow `json:"members"`
		}
		if err := json.Unmarshal(payload, &wrapped); err != nil {
			t.Fatalf("decode members from %s: %v", url, err)
		}
		rows = wrapped.Members
	}
	return rows
}

// TestGroupRosterIdentityFollowsTheGroupTheCallerAdministers: an admin of one
// group may open another group's roster, but sees only subject IDs there.
func TestGroupRosterIdentityFollowsTheGroupTheCallerAdministers(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member, err := services.Users.FindOrCreateUserWithName(context.Background(), "bob@valon.com", "Bob Builder")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	memberSubject := principal.UserSubjectID(member.ID)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)
	const ownGroup, otherGroup = "own-group", "other-group"

	authz := &serverTestAuthorizationProvider{relationships: []*proto.Relationship{
		testAuthorizationRelationship(adminSubject, "admin", "group", ownGroup),
		testAuthorizationRelationship(memberSubject, "member", "group", ownGroup),
		testAuthorizationRelationship(memberSubject, "member", "group", otherGroup),
	}}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", adminSubject, "")
		cfg.Authorization = authz
		cfg.Services = services
	})
	testutil.CloseOnCleanup(t, ts)

	own := memberIdentityFor(t, getMembers(t, ts.URL+"/api/v1/groups/"+ownGroup+"/admin/members", "admin-token"), memberSubject)
	if own.Email != "bob@valon.com" || own.DisplayName != "Bob Builder" {
		t.Fatalf("own group identity = %#v, want bob@valon.com / Bob Builder", own)
	}
	other := memberIdentityFor(t, getMembers(t, ts.URL+"/api/v1/groups/"+otherGroup+"/admin/members", "admin-token"), memberSubject)
	if other.Email != "" || other.DisplayName != "" {
		t.Fatalf("other group identity leaked to an admin of a different group: %#v", other)
	}
}

// TestOperatorResolvesEmailSubjectsOnARoster: resolving an email-shaped subject
// through the directory is user lookup, so only the operator role gets the name.
func TestOperatorResolvesEmailSubjectsOnARoster(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member, err := services.Users.FindOrCreateUserWithName(context.Background(), "bob@valon.com", "Bob Builder")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	emailSubject := principal.UserSubjectID(member.Email)
	adminSubject := principal.UserSubjectID(testCanonicalAdminUserID)

	authz := &serverTestAuthorizationProvider{relationships: []*proto.Relationship{
		testAuthorizationRelationship(adminSubject, "admin", "app", "g-issues"),
		testAuthorizationRelationship(emailSubject, "viewer", "app", "g-issues"),
		operatorGrant(),
	}}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("admin-token", adminSubject, "")
		cfg.Authorization = authz
		cfg.Services = services
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	got := memberIdentityFor(t, getMembers(t, ts.URL+"/api/v1/apps/g-issues/admin/members", "admin-token"), emailSubject)
	if got.Email != "bob@valon.com" || got.DisplayName != "Bob Builder" {
		t.Fatalf("operator identity = %#v, want bob@valon.com / Bob Builder", got)
	}
}

// TestPlatformAdminRosterShowsMemberIdentity: the platform-admins roster shares
// the member projection and must show the same names.
func TestPlatformAdminRosterShowsMemberIdentity(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member, err := services.Users.FindOrCreateUserWithName(context.Background(), "bob@valon.com", "Bob Builder")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	memberSubject := principal.UserSubjectID(member.ID)
	authz := &serverTestAuthorizationProvider{relationships: []*proto.Relationship{
		testAuthorizationRelationship(memberSubject, "admin", "gestalt", "gestalt"),
	}}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
		cfg.Services = services
	})
	testutil.CloseOnCleanup(t, ts)

	got := memberIdentityFor(t, getMembers(t, ts.URL+"/admin/api/v1/platform-admins", ""), memberSubject)
	if got.Email != "bob@valon.com" || got.DisplayName != "Bob Builder" {
		t.Fatalf("platform admin identity = %#v, want bob@valon.com / Bob Builder", got)
	}
}
