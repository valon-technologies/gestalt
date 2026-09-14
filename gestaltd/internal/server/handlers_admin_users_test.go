package server_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestAdminDirectoryRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []struct{ method, path, body string }{
		{http.MethodGet, "/users", ""},
		{http.MethodPost, "/users/lookup-emails", `{"userIds":[]}`},
	} {
		for _, scenario := range []struct {
			name                       string
			session, admin, authzError bool
			status                     int
		}{
			{"anonymous", false, true, false, http.StatusUnauthorized},
			{"non-admin", true, false, false, http.StatusForbidden},
			{"platform-admin-without-home", true, true, false, http.StatusOK},
			{"authorization-unavailable", true, true, true, http.StatusInternalServerError},
		} {
			t.Run(endpoint.path+"/"+scenario.name, func(t *testing.T) {
				t.Parallel()
				ts, authz := newAuthorizedAdminTestServerWithProvider(t, scenario.admin)
				testutil.CloseOnCleanup(t, ts)
				if scenario.authzError {
					authz.checkAccessErr = errors.New("unavailable")
				}
				req, err := http.NewRequest(endpoint.method, ts.URL+"/admin/api/v1"+endpoint.path, strings.NewReader(endpoint.body))
				if err != nil {
					t.Fatal(err)
				}
				if scenario.session {
					req.AddCookie(&http.Cookie{Name: "session_token", Value: "session-token"})
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = resp.Body.Close() }()
				body, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != scenario.status {
					t.Fatalf("status=%d want=%d: %s", resp.StatusCode, scenario.status, body)
				}
				if resp.StatusCode == http.StatusOK && resp.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("directory must not be cached")
				}
			})
		}
	}
}

func TestAdminDirectoryReturnsOnlyIDsAndEmails(t *testing.T) {
	t.Parallel()
	svc := testutil.NewStubServices(t)
	const alice = "11111111-1111-4111-8111-111111111111"
	const bob = "22222222-2222-4222-8222-222222222222"
	seedUserRecord(t, svc, bob, "bob@example.test", time.Now())
	seedUserRecord(t, svc, alice, "alice@example.test", time.Now())
	ts := newTestServer(t, func(cfg *server.Config) { cfg.Services = svc })
	testutil.CloseOnCleanup(t, ts)
	resp, err := http.Get(ts.URL + "/admin/api/v1/users")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var payload struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Users) != 2 || payload.Users[0]["id"] != alice || payload.Users[1]["id"] != bob {
		t.Fatalf("unexpected users: %#v", payload.Users)
	}
	for _, row := range payload.Users {
		if len(row) != 2 {
			t.Fatalf("unexpected user fields: %#v", row)
		}
	}

	lookupResp, err := http.Post(ts.URL+"/admin/api/v1/users/lookup-emails", "application/json", strings.NewReader(`{"userIds":["user:`+alice+`","`+alice+`","invalid","33333333-3333-4333-8333-333333333333"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lookupResp.Body.Close() }()
	var lookup struct {
		Emails map[string]string `json:"emails"`
	}
	if err := json.NewDecoder(lookupResp.Body).Decode(&lookup); err != nil {
		t.Fatal(err)
	}
	if len(lookup.Emails) != 1 || lookup.Emails[alice] != "alice@example.test" {
		t.Fatalf("unexpected lookup: %#v", lookup)
	}
}

func TestAdminDirectoryRejectsInvalidLookupPayload(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)
	tooMany, err := json.Marshal(map[string]any{"userIds": make([]string, 1001)})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"{", string(tooMany)} {
		resp, err := http.Post(ts.URL+"/admin/api/v1/users/lookup-emails", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d", resp.StatusCode)
		}
	}
}
