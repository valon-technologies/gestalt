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
