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

func TestAppAdminIdentitiesList(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   "service_account:slack-bot",
						}},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   "service_account:ci-runner",
						}},
					},
					Relation: "editor",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			},
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   "service_account:other-bot",
						}},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "other-app"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			},
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/identities", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET identities: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}

	var rows []struct {
		SubjectID   string `json:"subjectId"`
		DisplayName string `json:"displayName"`
		Role        string `json:"role"`
		Source      string `json:"source"`
		Mutable     bool   `json:"mutable"`
		Effective   bool   `json:"effective"`
	}
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("identities = %#v, want 2 service-account rows for g-issues", rows)
	}

	bySubject := map[string]struct {
		DisplayName string
		Role        string
		Source      string
		Mutable     bool
	}{}
	for _, row := range rows {
		if !row.Effective {
			t.Fatalf("unexpected row %#v", row)
		}
		bySubject[row.SubjectID] = struct {
			DisplayName string
			Role        string
			Source      string
			Mutable     bool
		}{DisplayName: row.DisplayName, Role: row.Role, Source: row.Source, Mutable: row.Mutable}
	}
	if got := bySubject["service_account:slack-bot"]; got.DisplayName != "slack-bot" || got.Role != "viewer" || got.Source != "dynamic" || !got.Mutable {
		t.Fatalf("slack-bot row = %#v", got)
	}
	if got := bySubject["service_account:ci-runner"]; got.DisplayName != "ci-runner" || got.Role != "editor" || got.Source != "static" || got.Mutable {
		t.Fatalf("ci-runner row = %#v", got)
	}
	if _, ok := bySubject[adminID]; ok {
		t.Fatalf("human subjects must not appear in identities: %#v", rows)
	}
}

func TestAppAdminIdentitiesListShadowsRuntimeDuplicate(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID("alice")
	const saID = "service_account:slack-bot"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   saID,
						}},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			},
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   saID,
						}},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/identities", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET identities: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}

	var rows []struct {
		Role       string `json:"role"`
		Source     string `json:"source"`
		Effective  bool   `json:"effective"`
		ShadowedBy string `json:"shadowedBy"`
		SubjectID  string `json:"subjectId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var staticViewer, dynamicViewer *struct {
		Role       string `json:"role"`
		Source     string `json:"source"`
		Effective  bool   `json:"effective"`
		ShadowedBy string `json:"shadowedBy"`
		SubjectID  string `json:"subjectId"`
	}
	for i := range rows {
		row := &rows[i]
		if row.SubjectID != saID || row.Role != "viewer" {
			continue
		}
		switch row.Source {
		case "static":
			staticViewer = row
		case "dynamic":
			dynamicViewer = row
		}
	}
	if staticViewer == nil || !staticViewer.Effective || staticViewer.ShadowedBy != "" {
		t.Fatalf("static viewer = %#v", staticViewer)
	}
	if dynamicViewer == nil || dynamicViewer.Effective || dynamicViewer.ShadowedBy != "static viewer grant" {
		t.Fatalf("dynamic viewer = %#v", dynamicViewer)
	}
}

func TestAppAdminIdentitiesListExcludesSubjectSet(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_SubjectSet{
							SubjectSet: &proto.SubjectSet{
								Resource: &proto.Resource{Type: "group", Id: "eng"},
								Relation: "member",
							},
						},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			},
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   "service_account:slack-bot",
						}},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			},
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/identities", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET identities: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}

	var rows []struct {
		SubjectID string `json:"subjectId"`
		Role      string `json:"role"`
	}
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(rows) != 1 || rows[0].SubjectID != "service_account:slack-bot" {
		t.Fatalf("identities = %#v, want only service_account:slack-bot", rows)
	}
}

func TestAppAdminIdentitiesListEmpty(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID("alice")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/identities", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET identities: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "[]" {
		t.Fatalf("empty identities body = %q, want []", got)
	}
}

func TestAppAdminIdentitiesFailsClosed(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID("alice")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = nil
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/identities", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET identities: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	// Without authorization, app-admin middleware fails closed before the handler.
	if response.StatusCode != http.StatusServiceUnavailable && response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d, want 503 or 403: %s", response.StatusCode, body)
	}
}

func TestAppAdminIdentitiesListForbiddenWithoutAdmin(t *testing.T) {
	t.Parallel()

	viewerID := principal.UserSubjectID("bob")
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(viewerID, "viewer", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("bob-token", viewerID, "")
		cfg.Authorization = authz
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/identities", nil)
	request.Header.Set("Authorization", "Bearer bob-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET identities: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d, want 403: %s", response.StatusCode, body)
	}
}
