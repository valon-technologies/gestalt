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

func TestAdminPlatformAdminsList(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "gestaltAdmin", "gestaltAdmin"),
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
					Relation: "admin",
					Resource: &proto.Resource{Type: "gestaltAdmin", Id: "gestaltAdmin"},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
			},
		},
	}
	authz.relationships[0].SourceLayer = proto.SourceLayer_SOURCE_LAYER_RUNTIME

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Authorization = authz
	})
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/platform-admins")
	if err != nil {
		t.Fatalf("GET platform-admins: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		Resource struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"resource"`
		Role    string `json:"role"`
		Members []struct {
			Role          string `json:"role"`
			Source        string `json:"source"`
			Mutable       bool   `json:"mutable"`
			SelectorKind  string `json:"selectorKind"`
			SelectorValue string `json:"selectorValue"`
		} `json:"members"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Resource.Type != "gestaltAdmin" || payload.Resource.ID != "gestaltAdmin" {
		t.Fatalf("resource = %#v", payload.Resource)
	}
	if payload.Role != "admin" {
		t.Fatalf("role = %q", payload.Role)
	}
	if len(payload.Members) != 2 {
		t.Fatalf("members = %#v", payload.Members)
	}
}

func TestAdminPlatformAdminsUnavailableWithoutAuthorization(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	testutil.CloseOnCleanup(t, ts)

	resp, err := http.Get(ts.URL + "/admin/api/v1/platform-admins")
	if err != nil {
		t.Fatalf("GET platform-admins: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 503: %s", resp.StatusCode, body)
	}
}
