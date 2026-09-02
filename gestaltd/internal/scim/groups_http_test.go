package scim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestSCIMGroupsCRUDAndUserGroups(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	})
	_, services, handler := newSCIMService(t, nil, authorization, cfg)
	schemas := scimRequest(t, handler, http.MethodGet, "/scim/v2/Schemas", testCurrentToken, nil)
	if schemas.Code != http.StatusOK || !bytes.Contains(schemas.Body.Bytes(), []byte(scim.GroupSchemaURN)) {
		t.Fatalf("Schemas = %d %s", schemas.Code, schemas.Body.String())
	}
	var schemaList struct {
		Resources []struct {
			ID         string `json:"id"`
			Attributes []struct {
				Name       string `json:"name"`
				Uniqueness string `json:"uniqueness"`
				Returned   string `json:"returned"`
				Sub        []struct {
					Name       string `json:"name"`
					Type       string `json:"type"`
					Mutability string `json:"mutability"`
				} `json:"subAttributes"`
			} `json:"attributes"`
		} `json:"Resources"`
	}
	if err := json.Unmarshal(schemas.Body.Bytes(), &schemaList); err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemaList.Resources {
		if schema.ID != scim.GroupSchemaURN {
			continue
		}
		for _, attribute := range schema.Attributes {
			if attribute.Name == "displayName" && attribute.Returned != "default" {
				t.Fatalf("Group.displayName returned = %q", attribute.Returned)
			}
			if attribute.Name == "externalId" && attribute.Uniqueness != "none" {
				t.Fatalf("Group.externalId uniqueness = %q", attribute.Uniqueness)
			}
			if attribute.Name != "members" {
				continue
			}
			for _, sub := range attribute.Sub {
				if (sub.Name == "value" || sub.Name == "$ref" || sub.Name == "type") && sub.Mutability != "immutable" {
					t.Fatalf("Group.members.%s mutability = %q", sub.Name, sub.Mutability)
				}
				if sub.Name == "value" && sub.Type != "string" {
					t.Fatalf("Group.members.value type = %q", sub.Type)
				}
			}
		}
	}
	alice, response := createUser(t, handler, testCurrentToken, "alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Alice = %d %s", response.Code, response.Body.String())
	}
	bob, response := createUser(t, handler, testCurrentToken, "bob@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Bob = %d %s", response.Code, response.Body.String())
	}

	create := map[string]any{
		"schemas":     []string{scim.GroupSchemaURN},
		"externalId":  "eng",
		"displayName": "Engineering",
		"members":     []map[string]any{{"value": alice.ID, "type": "User", "display": "client-controlled"}},
	}
	createdResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, create)
	created := decodeResponse[scim.Group](t, createdResponse)
	if createdResponse.Code != http.StatusCreated || created.ID == "" || created.DisplayName != "Engineering" || len(created.Members) != 1 || created.Members[0].Value != alice.ID || created.Members[0].Type != "User" || created.Members[0].Display == "client-controlled" || created.Meta.Version == "" {
		t.Fatalf("POST Group = %d %#v", createdResponse.Code, created)
	}
	duplicateExternal := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "externalId": "eng", "displayName": "Engineering Copy"})
	if duplicateExternal.Code != http.StatusCreated {
		t.Fatalf("duplicate Group externalId = %d %s", duplicateExternal.Code, duplicateExternal.Body.String())
	}
	if createdResponse.Header().Get("Location") != created.Meta.Location || createdResponse.Header().Get("ETag") != created.Meta.Version {
		t.Fatalf("Group headers = %#v", createdResponse.Header())
	}
	if createdResponse.Header().Get("Content-Location") != created.Meta.Location {
		t.Fatalf("Group Content-Location = %q", createdResponse.Header().Get("Content-Location"))
	}
	groupProjection := scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+created.ID+"?excludedAttributes=displayName", testCurrentToken, nil)
	if groupProjection.Code != http.StatusOK || bytes.Contains(groupProjection.Body.Bytes(), []byte(`"displayName"`)) || !bytes.Contains(groupProjection.Body.Bytes(), []byte(`"schemas"`)) || !bytes.Contains(groupProjection.Body.Bytes(), []byte(`"id"`)) {
		t.Fatalf("Group excludedAttributes projection = %d %s", groupProjection.Code, groupProjection.Body.String())
	}
	duplicateMemberPatch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": alice.ID, "type": "User"}}}}
	noOpResponse := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, duplicateMemberPatch, map[string]string{"If-Match": created.Meta.Version})
	noOp := decodeResponse[scim.Group](t, noOpResponse)
	if noOpResponse.Code != http.StatusOK {
		t.Fatalf("duplicate member add = %d %s", noOpResponse.Code, noOpResponse.Body.String())
	} else if noOp.Meta.Version == "" || noOp.Meta.Version != created.Meta.Version || !noOp.Meta.LastModified.Equal(created.Meta.LastModified) {
		t.Fatalf("duplicate member add changed metadata = before %#v after %#v", created.Meta, noOp.Meta)
	}
	coreAlice, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	projected := authorization.relationshipForUser(coreAlice.ID)
	if projected == nil || projected.GetTuple().GetResource().GetType() != "group" || projected.GetTuple().GetResource().GetId() != created.ID || projected.GetTuple().GetRelation() != "member" {
		t.Fatalf("projected Group membership = %#v", projected)
	}
	badType := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": bob.ID, "type": "Group"}}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, badType, map[string]string{"If-Match": created.Meta.Version}); response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched member type = %d %s", response.Code, response.Body.String())
	}
	secondService, _, _ := newSCIMService(t, services.DB, authorization, cfg)
	if _, err := scim.WrapAuthorization(authorization, services.Users, secondService).AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: projected}); err != nil {
		t.Fatalf("ordinary add to SCIM-managed Group: %v", err)
	}
	coreBob, err := services.Users.FindUserByEmail(context.Background(), "bob@valon.com")
	if err != nil {
		t.Fatalf("FindUserByEmail bob: %v", err)
	}
	groupBeforeExternal := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+created.ID, testCurrentToken, nil))
	bobBeforeExternal := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+bob.ID, testCurrentToken, nil))
	external := &proto.Relationship{Tuple: &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreBob.ID}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: created.ID}}}
	if _, err := scim.WrapAuthorization(authorization, services.Users, secondService).AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: external}); err != nil {
		t.Fatalf("ordinary add to SCIM Group: %v", err)
	}
	groupAfterExternal := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+created.ID, testCurrentToken, nil))
	bobAfterExternal := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+bob.ID, testCurrentToken, nil))
	if len(groupAfterExternal.Members) != 2 || groupAfterExternal.Meta.Version == groupBeforeExternal.Meta.Version || !groupAfterExternal.Meta.LastModified.After(groupBeforeExternal.Meta.LastModified) {
		t.Fatalf("external Group membership was not reflected = before %#v after %#v", groupBeforeExternal, groupAfterExternal)
	}
	if len(bobAfterExternal.Groups) != 1 || bobAfterExternal.Meta.Version == bobBeforeExternal.Meta.Version || !bobAfterExternal.Meta.LastModified.After(bobBeforeExternal.Meta.LastModified) {
		t.Fatalf("external User membership was not reflected = before %#v after %#v", bobBeforeExternal, bobAfterExternal)
	}
	created = groupAfterExternal
	filtered := scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups?filter="+url.QueryEscape(`displayName eq "engineering"`), testCurrentToken, nil)
	groups := decodeResponse[struct {
		TotalResults int          `json:"totalResults"`
		Resources    []scim.Group `json:"Resources"`
	}](t, filtered)
	if filtered.Code != http.StatusOK || groups.TotalResults != 1 || groups.Resources[0].ID != created.ID {
		t.Fatalf("filtered Groups = %d %#v", filtered.Code, groups)
	}

	aliceRead := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+alice.ID, testCurrentToken, nil))
	if len(aliceRead.Groups) != 1 || aliceRead.Groups[0].Value != created.ID || aliceRead.Groups[0].Display != "" || aliceRead.Groups[0].Type != "direct" {
		t.Fatalf("User.groups after create = %#v", aliceRead.Groups)
	}

	patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": bob.ID, "type": "User"}}}}
	patchedResponse := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, patch, map[string]string{"If-Match": created.Meta.Version})
	patched := decodeResponse[scim.Group](t, patchedResponse)
	if patchedResponse.Code != http.StatusOK || len(patched.Members) != 2 || patched.Meta.Version == "" {
		t.Fatalf("PATCH add member = %d %#v", patchedResponse.Code, patched)
	}

	removeOne := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": `members[value eq "` + alice.ID + `"]`}}}
	removedResponse := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, removeOne, map[string]string{"If-Match": patched.Meta.Version})
	removed := decodeResponse[scim.Group](t, removedResponse)
	if removedResponse.Code != http.StatusOK || len(removed.Members) != 1 || removed.Members[0].Value != bob.ID {
		t.Fatalf("PATCH filtered remove = %d %#v", removedResponse.Code, removed)
	}
	missing := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, removeOne, map[string]string{"If-Match": removed.Meta.Version})
	if payload := decodeResponse[testErrorResponse](t, missing); missing.Code != http.StatusBadRequest || payload.SCIMType != "noTarget" {
		t.Fatalf("PATCH missing member = %d %#v", missing.Code, payload)
	}

	clear := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members", "value": []map[string]any{{"value": bob.ID}}}}}
	clearedResponse := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, clear, map[string]string{"If-Match": removed.Meta.Version})
	cleared := decodeResponse[scim.Group](t, clearedResponse)
	if clearedResponse.Code != http.StatusOK || len(cleared.Members) != 0 {
		t.Fatalf("PATCH unfiltered remove = %d %#v", clearedResponse.Code, cleared)
	}

	stale := scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+created.ID, testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Renamed"}, map[string]string{"If-Match": created.Meta.Version})
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale Group PUT = %d %s", stale.Code, stale.Body.String())
	}
	readd := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": bob.ID}}}}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, readd, map[string]string{"If-Match": cleared.Meta.Version}); response.Code != http.StatusOK {
		t.Fatalf("PATCH member before delete = %d %s", response.Code, response.Body.String())
	}

	authorization.setFailures(false, true)
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Groups/"+created.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed DELETE Group = %d %s", response.Code, response.Body.String())
	}
	authorization.setFailures(false, false)
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Groups/"+created.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE Group = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Groups/"+created.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNotFound {
		t.Fatalf("DELETE tombstoned Group = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+created.ID, testCurrentToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("deleted Group GET = %d", response.Code)
	}
	if user := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+bob.ID, testCurrentToken, nil)); len(user.Groups) != 0 {
		t.Fatalf("User.groups after Group delete = %#v", user.Groups)
	}
	coreBob, err = services.Users.FindUserByEmail(context.Background(), "bob@valon.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	if relationship := authorization.relationshipForUser(coreBob.ID); relationship != nil {
		t.Fatalf("projected membership after Group delete = %#v", relationship)
	}
}

func TestSCIMGroupsSupportNestedMembershipAndNamespaceIsolation(t *testing.T) {
	t.Parallel()

	clients := map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
		"entra": {
			Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}},
		},
	}
	_, _, handler := newSCIMService(t, nil, newRecordingAuthorization(), testSCIMConfig(clients))
	user, response := createUser(t, handler, testCurrentToken, "nested@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create nested user = %d %s", response.Code, response.Body.String())
	}
	childResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Child"})
	child := decodeResponse[scim.Group](t, childResponse)
	if childResponse.Code != http.StatusCreated {
		t.Fatalf("create child = %d %s", childResponse.Code, childResponse.Body.String())
	}
	addUser := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}}}}
	child = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+child.ID, testCurrentToken, addUser, map[string]string{"If-Match": child.Meta.Version}))
	parentResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Parent", "members": []map[string]any{{"value": child.ID, "type": "Group"}}})
	parent := decodeResponse[scim.Group](t, parentResponse)
	if parentResponse.Code != http.StatusCreated {
		t.Fatalf("create parent = %d %s", parentResponse.Code, parentResponse.Body.String())
	}
	if len(parent.Members) != 1 || parent.Members[0].Value != child.ID {
		t.Fatalf("parent members = %#v", parent.Members)
	}
	readUser := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	if len(readUser.Groups) != 2 {
		t.Fatalf("nested User.groups = %#v", readUser.Groups)
	}
	for _, group := range readUser.Groups {
		if group.Value == child.ID && group.Type != "direct" || group.Value == parent.ID && group.Type != "indirect" {
			t.Fatalf("nested User.groups types = %#v", readUser.Groups)
		}
	}
	addDirect := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}}}}
	parent = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+parent.ID, testCurrentToken, addDirect, map[string]string{"If-Match": parent.Meta.Version}))
	readUser = decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	for _, group := range readUser.Groups {
		if group.Value == parent.ID && group.Type != "direct" {
			t.Fatalf("direct membership did not override indirect membership: %#v", readUser.Groups)
		}
	}
	removeDirect := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": `members[value eq "` + user.ID + `"]`}}}
	parent = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+parent.ID, testCurrentToken, removeDirect, map[string]string{"If-Match": parent.Meta.Version}))
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Groups/"+child.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE nested child = %d %s", response.Code, response.Body.String())
	}
	parentAfterDelete := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+parent.ID, testCurrentToken, nil))
	if len(parentAfterDelete.Members) != 0 {
		t.Fatalf("parent members after child delete = %#v", parentAfterDelete.Members)
	}

	otherUser, response := createUser(t, handler, "entra-token", "other@example.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create other namespace user = %d %s", response.Code, response.Body.String())
	}
	foreign := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, "entra-token", nil)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("cross-client User GET = %d", foreign.Code)
	}
	foreignGroup := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", "entra-token", map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Foreign", "members": []map[string]any{{"value": user.ID}}})
	if foreignGroup.Code != http.StatusBadRequest {
		t.Fatalf("cross-client Group member = %d %s", foreignGroup.Code, foreignGroup.Body.String())
	}
	_ = otherUser
}

func TestSCIMUserDeleteRemovesGroupMembership(t *testing.T) {
	t.Parallel()

	_, _, handler := newSCIMService(t, nil, newRecordingAuthorization(), testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))
	user, response := createUser(t, handler, testCurrentToken, "remove-me@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Cleanup", "members": []map[string]any{{"value": user.ID}}})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Users/"+user.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
		t.Fatalf("delete user = %d %s", response.Code, response.Body.String())
	}
	updated := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if len(updated.Members) != 0 {
		t.Fatalf("group members after user delete = %#v", updated.Members)
	}
}

func TestSCIMUserDeleteRemovesConfiguredTuplesAndGroupReferences(t *testing.T) {
	t.Parallel()
	authorization := newRecordingAuthorization()
	projections := []config.SCIMRelationshipConfig{employeeProjection(), {Relation: "member", Resource: config.AuthorizationResourceDef{Type: "group", ID: "engineering"}}}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient([]string{"valon.com"}, projections...)})
	_, services, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "delete-all@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "delete-all@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Cleanup", "members": []map[string]any{{"value": user.ID}}})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodDelete, "/scim/v2/Users/"+user.ID, testCurrentToken, nil, map[string]string{"If-Match": "*"}); response.Code != http.StatusNoContent {
		t.Fatalf("delete user = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("deleted user GET = %d", response.Code)
	}
	updated := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if len(updated.Members) != 0 || authorization.relationshipForUser(coreUser.ID) != nil {
		t.Fatalf("group references after user deletion = %#v", updated.Members)
	}
}

func TestSCIMGroupMembershipFailureLeavesLiveStateForRetry(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "recover@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	authorization.setFailures(true, false)
	response = scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Recovery", "members": []map[string]any{{"value": user.ID}},
	})
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("failed Group projection = %d %s", response.Code, response.Body.String())
	}
	authorization.setFailures(false, false)
	listed := decodeResponse[struct {
		Resources []scim.Group `json:"Resources"`
	}](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups?filter="+url.QueryEscape(`displayName eq "Recovery"`), testCurrentToken, nil))
	if len(listed.Resources) != 1 || len(listed.Resources[0].Members) != 0 {
		t.Fatalf("live Group after failed create = %#v", listed.Resources)
	}
	group := listed.Resources[0]
	second, response := createUser(t, handler, testCurrentToken, "recover-second@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create second user = %d %s", response.Code, response.Body.String())
	}
	authorization.setFailures(true, false)
	addSecond := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": second.ID}}}}
	response = scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, addSecond, map[string]string{"If-Match": group.Meta.Version})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed Group update = %d %s", response.Code, response.Body.String())
	}
	visible := decodeResponse[struct {
		TotalResults int          `json:"totalResults"`
		Resources    []scim.Group `json:"Resources"`
	}](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups?filter="+url.QueryEscape(`displayName eq "Recovery"`), testCurrentToken, nil))
	if visible.TotalResults != 1 || len(visible.Resources) != 1 || len(visible.Resources[0].Members) != 0 {
		t.Fatalf("live Group after failed update = %#v", visible)
	}
	authorization.setFailures(false, false)
	retried := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, addSecond, map[string]string{"If-Match": group.Meta.Version})
	if updated := decodeResponse[scim.Group](t, retried); retried.Code != http.StatusOK || len(updated.Members) != 1 {
		t.Fatalf("retried Group update = %d %#v", retried.Code, updated)
	}
}

func TestSCIMGroupMembershipFailureAtEachDiffPositionCanRetry(t *testing.T) {
	t.Parallel()
	for failAt := 1; failAt <= 3; failAt++ {
		t.Run(fmt.Sprintf("failure-%d", failAt), func(t *testing.T) {
			authorization := newRecordingAuthorization()
			cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
			_, _, handler := newSCIMService(t, nil, authorization, cfg)
			members := make([]map[string]any, 0, 3)
			for i, name := range []string{"position-a@valon.com", "position-b@valon.com", "position-c@valon.com"} {
				user, response := createUser(t, handler, testCurrentToken, name, true, nil)
				if response.Code != http.StatusCreated {
					t.Fatalf("create user %d = %d %s", i, response.Code, response.Body.String())
				}
				members = append(members, map[string]any{"value": user.ID})
			}
			created := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Positioned"})
			group := decodeResponse[scim.Group](t, created)
			if created.Code != http.StatusCreated {
				t.Fatalf("create group = %d %s", created.Code, created.Body.String())
			}
			patch := map[string]any{"schemas": []string{scim.PatchSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": members}}}
			authorization.setFailureAt(failAt, 0)
			failed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, patch, map[string]string{"If-Match": group.Meta.Version})
			if failed.Code != http.StatusServiceUnavailable {
				t.Fatalf("failure at position %d = %d %s", failAt, failed.Code, failed.Body.String())
			}
			live := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
			if len(live.Members) != failAt-1 {
				t.Fatalf("live members after position %d failure = %d, want %d", failAt, len(live.Members), failAt-1)
			}
			authorization.setFailures(false, false)
			retried := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, patch, map[string]string{"If-Match": "*"})
			if retried.Code != http.StatusOK {
				t.Fatalf("retry after position %d = %d %s", failAt, retried.Code, retried.Body.String())
			}
			complete := decodeResponse[scim.Group](t, retried)
			if len(complete.Members) != len(members) {
				t.Fatalf("members after retry = %d, want %d", len(complete.Members), len(members))
			}
		})
	}
}
