package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAppAdminMembersList(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   viewerID,
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
							Id:   "service_account:slack-bot",
						}},
					},
					Relation: "viewer",
					Resource: &proto.Resource{Type: "app", Id: "g-issues"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
			testAuthorizationRelationship(adminID, "admin", "app", "other-app"),
		},
	}
	// Stamp static layer on the helper-built admin row.
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}

	var rows []struct {
		Role          string `json:"role"`
		Source        string `json:"source"`
		Mutable       bool   `json:"mutable"`
		Effective     bool   `json:"effective"`
		SelectorKind  string `json:"selectorKind"`
		SelectorValue string `json:"selectorValue"`
		SubjectID     string `json:"subjectId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("members = %#v, want 2 human rows for g-issues (service accounts excluded)", rows)
	}

	bySubject := map[string]struct {
		Role    string
		Source  string
		Mutable bool
	}{}
	for _, row := range rows {
		if !row.Effective || row.SelectorKind != "subject_id" {
			t.Fatalf("unexpected row %#v", row)
		}
		bySubject[row.SubjectID] = struct {
			Role    string
			Source  string
			Mutable bool
		}{Role: row.Role, Source: row.Source, Mutable: row.Mutable}
	}
	if got := bySubject[adminID]; got.Role != "admin" || got.Source != "static" || got.Mutable {
		t.Fatalf("admin row = %#v", got)
	}
	if got := bySubject[viewerID]; got.Role != "viewer" || got.Source != "static" || got.Mutable {
		t.Fatalf("viewer row = %#v", got)
	}
	if _, ok := bySubject["service_account:slack-bot"]; ok {
		t.Fatalf("service account must not appear in members: %#v", rows)
	}
}

func TestAppAdminMembersListShadowsRuntimeDuplicate(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   viewerID,
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
							Id:   viewerID,
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
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
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
		if row.SubjectID != viewerID || row.Role != "viewer" {
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

func TestAppAdminMembersListSubjectSet(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
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
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET status = %d: %s", response.StatusCode, body)
	}

	var rows []struct {
		Role          string `json:"role"`
		SelectorKind  string `json:"selectorKind"`
		SelectorValue string `json:"selectorValue"`
		SubjectID     string `json:"subjectId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.SelectorKind == "subject_set" && row.SelectorValue == "group:eng#member" && row.Role == "viewer" && row.SubjectID == "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("subject_set row missing: %#v", rows)
	}
}

func TestAppAdminMembersListForbiddenWithoutAdmin(t *testing.T) {
	t.Parallel()

	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(viewerID, "viewer", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("bob-token", viewerID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/members", nil)
	request.Header.Set("Authorization", "Bearer bob-token")
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

func TestAppAdminMembersSetAddsRuntimeGrant(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/members", bytes.NewBufferString(`{"subjectId":"`+viewerID+`","role":"viewer"}`))
	request.Header.Set("Authorization", "Bearer alice-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("POST members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST status = %d: %s", response.StatusCode, body)
	}

	var body struct {
		Changed bool `json:"changed"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Changed {
		t.Fatal("changed = false, want true")
	}
	authz.mu.Lock()
	defer authz.mu.Unlock()
	if len(authz.addRelationshipRequests) != 1 {
		t.Fatalf("AddRelationship calls = %d, want 1", len(authz.addRelationshipRequests))
	}
	added := authz.addRelationshipRequests[0].GetRelationship()
	if added.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
		t.Fatalf("source layer = %v, want runtime", added.GetSourceLayer())
	}
	if tuple := added.GetTuple(); tuple.GetResource().GetId() != "g-issues" || tuple.GetRelation() != "viewer" || tuple.GetTarget().GetSubject().GetId() != viewerID {
		t.Fatalf("added tuple = %#v", tuple)
	}
}

func TestAppAdminMembersRemoveDeletesRuntimeGrants(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "app", "g-issues"),
			testAuthorizationRelationship(viewerID, "viewer", "app", "g-issues"),
			testAuthorizationRelationship(viewerID, "editor", "app", "g-issues"),
		},
	}
	for i := range authz.relationships {
		authz.relationships[i].SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/apps/g-issues/admin/members", bytes.NewBufferString(`{"subjectId":"`+viewerID+`"}`))
	request.Header.Set("Authorization", "Bearer alice-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("DELETE members: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("DELETE status = %d: %s", response.StatusCode, body)
	}

	var body struct {
		RemovedRoles []string `json:"removedRoles"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.RemovedRoles) != 2 {
		t.Fatalf("removedRoles = %#v, want two roles", body.RemovedRoles)
	}
	authz.mu.Lock()
	defer authz.mu.Unlock()
	if len(authz.deleteRelationshipRequests) != 2 {
		t.Fatalf("DeleteRelationship calls = %d, want 2", len(authz.deleteRelationshipRequests))
	}
	if len(authz.relationships) != 1 || authz.relationships[0].GetTuple().GetTarget().GetSubject().GetId() != adminID {
		t.Fatalf("remaining relationships = %#v", authz.relationships)
	}
}
