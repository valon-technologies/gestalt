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

func TestGroupAdminListAndGet(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	memberID := principal.UserSubjectID(testCanonicalViewerUserID)
	groupID := "servicemacusa-employees"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "group", groupID),
			{
				Tuple: &proto.RelationshipTuple{
					Target: &proto.RelationshipTarget{
						Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
							Type: "subject",
							Id:   memberID,
						}},
					},
					Relation: "member",
					Resource: &proto.Resource{Type: "group", Id: groupID},
				},
				SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
			},
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	listRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups", nil)
	listRequest.Header.Set("Authorization", "Bearer alice-token")
	listResponse, err := http.DefaultClient.Do(listRequest)
	if err != nil {
		t.Fatalf("GET groups: %v", err)
	}
	defer func() { _ = listResponse.Body.Close() }()
	if listResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResponse.Body)
		t.Fatalf("GET groups status = %d: %s", listResponse.StatusCode, body)
	}

	var groups []struct {
		ID          string `json:"id"`
		MemberCount int    `json:"memberCount"`
		ScimManaged bool   `json:"scimManaged"`
		Editable    bool   `json:"editable"`
		CanAdmin    bool   `json:"canAdmin"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	if len(groups) != 1 || groups[0].ID != groupID || groups[0].MemberCount != 1 || !groups[0].CanAdmin {
		t.Fatalf("groups = %#v", groups)
	}

	getRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups/"+groupID, nil)
	getRequest.Header.Set("Authorization", "Bearer alice-token")
	getResponse, err := http.DefaultClient.Do(getRequest)
	if err != nil {
		t.Fatalf("GET group: %v", err)
	}
	defer func() { _ = getResponse.Body.Close() }()
	if getResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResponse.Body)
		t.Fatalf("GET group status = %d: %s", getResponse.StatusCode, body)
	}

	var summary struct {
		ID          string `json:"id"`
		MemberCount int    `json:"memberCount"`
		CanAdmin    bool   `json:"canAdmin"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&summary); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if summary.ID != groupID || summary.MemberCount != 1 || !summary.CanAdmin {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestGroupAdminMembersMutations(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	memberID := principal.UserSubjectID(testCanonicalViewerUserID)
	groupID := "servicemacusa-employees"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "group", groupID),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	addRequest, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/groups/"+groupID+"/admin/members",
		bytes.NewBufferString(`{"subjectId":"`+memberID+`"}`),
	)
	addRequest.Header.Set("Authorization", "Bearer alice-token")
	addRequest.Header.Set("Content-Type", "application/json")
	addResponse, err := http.DefaultClient.Do(addRequest)
	if err != nil {
		t.Fatalf("POST member: %v", err)
	}
	defer func() { _ = addResponse.Body.Close() }()
	if addResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(addResponse.Body)
		t.Fatalf("POST member status = %d: %s", addResponse.StatusCode, body)
	}

	listRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups/"+groupID+"/admin/members", nil)
	listRequest.Header.Set("Authorization", "Bearer alice-token")
	listResponse, err := http.DefaultClient.Do(listRequest)
	if err != nil {
		t.Fatalf("GET members: %v", err)
	}
	defer func() { _ = listResponse.Body.Close() }()
	if listResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResponse.Body)
		t.Fatalf("GET members status = %d: %s", listResponse.StatusCode, body)
	}

	var rows []struct {
		SubjectID string `json:"subjectId"`
		Role      string `json:"role"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&rows); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(rows) != 1 || rows[0].SubjectID != memberID || rows[0].Role != "member" {
		t.Fatalf("members = %#v", rows)
	}

	deleteRequest, _ := http.NewRequest(
		http.MethodDelete,
		ts.URL+"/api/v1/groups/"+groupID+"/admin/members",
		bytes.NewBufferString(`{"subjectId":"`+memberID+`"}`),
	)
	deleteRequest.Header.Set("Authorization", "Bearer alice-token")
	deleteRequest.Header.Set("Content-Type", "application/json")
	deleteResponse, err := http.DefaultClient.Do(deleteRequest)
	if err != nil {
		t.Fatalf("DELETE member: %v", err)
	}
	defer func() { _ = deleteResponse.Body.Close() }()
	if deleteResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(deleteResponse.Body)
		t.Fatalf("DELETE member status = %d: %s", deleteResponse.StatusCode, body)
	}
}

func TestGroupAdminCreatePersistsDisplayName(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	groupID := "servicemacusa-employees"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "authorization", "authorization"),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	createRequest, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/groups",
		bytes.NewBufferString(`{"id":"`+groupID+`","displayName":"ServiceMac employees"}`),
	)
	createRequest.Header.Set("Authorization", "Bearer alice-token")
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse, err := http.DefaultClient.Do(createRequest)
	if err != nil {
		t.Fatalf("POST group: %v", err)
	}
	defer func() { _ = createResponse.Body.Close() }()
	if createResponse.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResponse.Body)
		t.Fatalf("POST group status = %d: %s", createResponse.StatusCode, body)
	}

	var created struct {
		Group struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			CanAdmin    bool   `json:"canAdmin"`
		} `json:"group"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode created group: %v", err)
	}
	if created.Group.ID != groupID || created.Group.DisplayName != "ServiceMac employees" || !created.Group.CanAdmin {
		t.Fatalf("created group = %#v", created.Group)
	}
	if got := authz.relationships[len(authz.relationships)-1].GetProperties().GetFields()["displayName"].GetStringValue(); got != "ServiceMac employees" {
		t.Fatalf("stored displayName = %q", got)
	}

	getRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups/"+groupID, nil)
	getRequest.Header.Set("Authorization", "Bearer alice-token")
	getResponse, err := http.DefaultClient.Do(getRequest)
	if err != nil {
		t.Fatalf("GET group: %v", err)
	}
	defer func() { _ = getResponse.Body.Close() }()
	if getResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResponse.Body)
		t.Fatalf("GET group status = %d: %s", getResponse.StatusCode, body)
	}

	var summary struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&summary); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if summary.DisplayName != "ServiceMac employees" {
		t.Fatalf("GET group displayName = %q", summary.DisplayName)
	}
}

func TestGroupAdminCreateRejectsExistingGroup(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	groupID := "servicemacusa-employees"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "authorization", "authorization"),
			testAuthorizationRelationship("user:existing@example.com", "member", "group", groupID),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	createRequest, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/groups",
		bytes.NewBufferString(`{"id":"`+groupID+`","displayName":"ServiceMac employees"}`),
	)
	createRequest.Header.Set("Authorization", "Bearer alice-token")
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse, err := http.DefaultClient.Do(createRequest)
	if err != nil {
		t.Fatalf("POST group: %v", err)
	}
	defer func() { _ = createResponse.Body.Close() }()
	if createResponse.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(createResponse.Body)
		t.Fatalf("POST existing group status = %d, want 409: %s", createResponse.StatusCode, body)
	}
	if len(authz.addRelationshipRequests) != 0 {
		t.Fatalf("add relationship requests = %d, want 0", len(authz.addRelationshipRequests))
	}
}

func TestGroupAdminScimGroupIsReadOnly(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	groupID := "e7dce358-8291-431f-baf0-fdb8a10b4252"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "group", groupID),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", adminID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
		cfg.ScimManagedGroupIDs = map[string]struct{}{groupID: {}}
	})
	testutil.CloseOnCleanup(t, ts)

	getRequest, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups/"+groupID, nil)
	getRequest.Header.Set("Authorization", "Bearer alice-token")
	getResponse, err := http.DefaultClient.Do(getRequest)
	if err != nil {
		t.Fatalf("GET group: %v", err)
	}
	defer func() { _ = getResponse.Body.Close() }()
	if getResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResponse.Body)
		t.Fatalf("GET group status = %d: %s", getResponse.StatusCode, body)
	}

	var summary struct {
		ScimManaged bool `json:"scimManaged"`
		Editable    bool `json:"editable"`
		CanAdmin    bool `json:"canAdmin"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&summary); err != nil {
		t.Fatalf("decode group: %v", err)
	}
	if !summary.ScimManaged || summary.Editable || summary.CanAdmin {
		t.Fatalf("summary = %#v", summary)
	}

	addRequest, _ := http.NewRequest(
		http.MethodPost,
		ts.URL+"/api/v1/groups/"+groupID+"/admin/members",
		bytes.NewBufferString(`{"subjectId":"`+principal.UserSubjectID(testCanonicalViewerUserID)+`"}`),
	)
	addRequest.Header.Set("Authorization", "Bearer alice-token")
	addRequest.Header.Set("Content-Type", "application/json")
	addResponse, err := http.DefaultClient.Do(addRequest)
	if err != nil {
		t.Fatalf("POST member: %v", err)
	}
	defer func() { _ = addResponse.Body.Close() }()
	if addResponse.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(addResponse.Body)
		t.Fatalf("POST member status = %d: %s", addResponse.StatusCode, body)
	}
}

func TestGroupAdminListAllowsDelegatedAdmin(t *testing.T) {
	t.Parallel()

	adminID := principal.UserSubjectID(testCanonicalAdminUserID)
	viewerID := principal.UserSubjectID(testCanonicalViewerUserID)
	groupID := "servicemacusa-employees"
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(adminID, "admin", "group", groupID),
			testAuthorizationRelationship(viewerID, "viewer", "app", "g-issues"),
		},
	}

	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("viewer-token", viewerID, "")
		cfg.Authorization = authz
		cfg.AppDefs = appAdminTestAppDefs()
	})
	testutil.CloseOnCleanup(t, ts)

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups", nil)
	request.Header.Set("Authorization", "Bearer viewer-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET groups: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET groups status = %d: %s", response.StatusCode, body)
	}

	authz.relationships = append(authz.relationships,
		testAuthorizationRelationship(viewerID, "admin", "group", groupID),
	)

	request, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/v1/groups", nil)
	request.Header.Set("Authorization", "Bearer viewer-token")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET groups after grant: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET groups status = %d: %s", response.StatusCode, body)
	}
}
