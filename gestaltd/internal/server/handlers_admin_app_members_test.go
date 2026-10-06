package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// TestAdminAppMembersListShowsNames: a platform admin who is not an admin of
// the app can read its roster with each person's name and email, and cannot
// change it through the same path.
func TestAdminAppMembersListShowsNames(t *testing.T) {
	t.Parallel()

	services := testutil.NewStubServices(t)
	member := seedUserWithName(t, services, "bob@valon.com", "Bob Builder")
	memberSubject := principal.UserSubjectID(member.ID)
	authz := &serverTestAuthorizationProvider{relationships: []*proto.Relationship{
		testAuthorizationRelationship(memberSubject, "viewer", "app", "g-issues"),
	}}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
		cfg.Services = services
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/apps/g-issues/members")
	if err != nil {
		t.Fatalf("GET admin app members: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var rows []memberIdentityRow
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := memberIdentityFor(t, rows, memberSubject)
	if got.Email != "bob@valon.com" || got.DisplayName != "Bob Builder" {
		t.Fatalf("member identity = %#v, want bob@valon.com / Bob Builder", got)
	}

	write, err := http.Post(ts.URL+"/admin/api/v1/apps/g-issues/members", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST admin app members: %v", err)
	}
	defer func() { _ = write.Body.Close() }()
	if write.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405: the platform route is read-only", write.StatusCode)
	}
}
