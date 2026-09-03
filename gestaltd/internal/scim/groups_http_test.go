package scim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"testing"

	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
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
	unsupportedPatch := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+created.ID, testCurrentToken, map[string]any{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"}, "Operations": []map[string]any{}})
	if payload := decodeResponse[testErrorResponse](t, unsupportedPatch); unsupportedPatch.Code != http.StatusNotImplemented || payload.Status != "501" {
		t.Fatalf("unsupported Group PATCH = %d %#v", unsupportedPatch.Code, payload)
	}
	coreAlice, err := services.Users.FindUserByEmail(context.Background(), "alice@valon.com")
	if err != nil {
		t.Fatalf("FindUserByEmail: %v", err)
	}
	projected := authorization.relationshipForUser(coreAlice.ID)
	if projected == nil || projected.GetTuple().GetResource().GetType() != "group" || projected.GetTuple().GetResource().GetId() != created.ID || projected.GetTuple().GetRelation() != "member" {
		t.Fatalf("projected Group membership = %#v", projected)
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
	external := &proto.Relationship{Tuple: &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreBob.ID}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: created.ID}}, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}
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
	replacedResponse := scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+created.ID, testCurrentToken, map[string]any{
		"schemas":     []string{scim.GroupSchemaURN},
		"displayName": "Engineering",
		"members":     []map[string]any{{"value": alice.ID, "type": "User", "display": "ignored"}, {"value": bob.ID, "type": "User"}},
	}, map[string]string{"If-Match": created.Meta.Version})
	replaced := decodeResponse[scim.Group](t, replacedResponse)
	if replacedResponse.Code != http.StatusOK || replaced.ExternalID != "" || len(replaced.Members) != 2 || replaced.Members[0].Display != "" {
		t.Fatalf("PUT Group replacement = %d %#v", replacedResponse.Code, replaced)
	}
	created = replaced
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
	authorization := newRecordingAuthorization()
	service, services, handler := newSCIMService(t, nil, authorization, testSCIMConfig(clients))
	user, response := createUser(t, handler, testCurrentToken, "nested@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create nested user = %d %s", response.Code, response.Body.String())
	}
	childResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Child"})
	child := decodeResponse[scim.Group](t, childResponse)
	if childResponse.Code != http.StatusCreated {
		t.Fatalf("create child = %d %s", childResponse.Code, childResponse.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "nested@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	childTuple := &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreUser.ID}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: child.ID}}
	if _, err := scim.WrapAuthorization(authorization, services.Users, service).AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: &proto.Relationship{Tuple: childTuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}}); err != nil {
		t.Fatal(err)
	}
	child = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+child.ID, testCurrentToken, nil))
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
	parentTuple := &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreUser.ID}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: parent.ID}}
	gate := scim.WrapAuthorization(authorization, services.Users, service)
	if _, err := gate.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: &proto.Relationship{Tuple: parentTuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}}); err != nil {
		t.Fatal(err)
	}
	parent = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+parent.ID, testCurrentToken, nil))
	readUser = decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
	for _, group := range readUser.Groups {
		if group.Value == parent.ID && group.Type != "direct" {
			t.Fatalf("direct membership did not override indirect membership: %#v", readUser.Groups)
		}
	}
	if _, err := gate.DeleteRelationship(context.Background(), &proto.DeleteRelationshipRequest{RelationshipTuple: parentTuple}); err != nil {
		t.Fatal(err)
	}
	parent = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+parent.ID, testCurrentToken, nil))
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

func TestSCIMGroupsExposeOnlyRuntimeMembership(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	service, services, handler := newSCIMService(t, nil, authorization, testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))
	user, response := createUser(t, handler, testCurrentToken, "runtime-membership@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Runtime membership",
	})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "runtime-membership@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	staticRelationship := runtimeGroupMember(coreUser.ID, group.ID)
	staticRelationship.SourceLayer = proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG
	gate := scim.WrapAuthorization(authorization, services.Users, service)
	if _, err := gate.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: staticRelationship}); err != nil {
		t.Fatalf("add static relationship = %v", err)
	}
	staticRead := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if len(staticRead.Members) != 0 {
		t.Fatalf("static relationship exposed as members = %#v", staticRead.Members)
	}
	if staticRead.Meta.Version != group.Meta.Version || !staticRead.Meta.LastModified.Equal(group.Meta.LastModified) {
		t.Fatalf("static relationship changed SCIM metadata: before=%#v after=%#v", group.Meta, staticRead.Meta)
	}
	if _, err := gate.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: runtimeGroupMember(coreUser.ID, group.ID)}); err != nil {
		t.Fatalf("add runtime relationship = %v", err)
	}
	runtimeRead := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if len(runtimeRead.Members) != 1 || runtimeRead.Members[0].Value != user.ID {
		t.Fatalf("runtime relationship not exposed as member = %#v", runtimeRead.Members)
	}
}

func TestSCIMGroupMemberFilterDistinguishesNotFoundAndDatastoreError(t *testing.T) {
	t.Parallel()

	filter := url.QueryEscape(`members[value eq "missing-user"]`)
	_, _, healthyHandler := newSCIMService(t, nil, newRecordingAuthorization(), testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))
	missing := scimRequest(t, healthyHandler, http.MethodGet, "/scim/v2/Groups?startIndex=10&filter="+filter, testCurrentToken, nil)
	if missing.Code != http.StatusOK {
		t.Fatalf("missing member filter = %d %s", missing.Code, missing.Body.String())
	}
	missingList := decodeResponse[struct {
		TotalResults int          `json:"totalResults"`
		StartIndex   int          `json:"startIndex"`
		Resources    []scim.Group `json:"Resources"`
	}](t, missing)
	if missingList.TotalResults != 0 || missingList.StartIndex != 1 || missingList.Resources == nil {
		t.Fatalf("missing member filter result = %#v", missingList)
	}

	db := &coretesting.StubIndexedDB{Err: errors.New("member lookup datastore unavailable")}
	_, _, failingHandler := newSCIMService(t, db, newRecordingAuthorization(), testSCIMConfig(map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
	}))
	failure := scimRequest(t, failingHandler, http.MethodGet, "/scim/v2/Groups?filter="+filter, testCurrentToken, nil)
	if failure.Code != http.StatusServiceUnavailable {
		t.Fatalf("member lookup datastore error = %d %s", failure.Code, failure.Body.String())
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

func TestSCIMRelationshipPropertiesDoNotCauseMemberDiff(t *testing.T) {
	t.Parallel()

	authorization := newRecordingAuthorization()
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, services, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "physical-variants@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Physical variants", "members": []map[string]any{{"value": user.ID}},
	})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "physical-variants@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	base := authorization.relationshipForGroupUser(coreUser.ID, group.ID)
	if base == nil {
		t.Fatal("created relationship is missing")
	}
	for _, property := range []string{"one", "two"} {
		variant := gproto.Clone(base).(*proto.Relationship)
		variant.Tuple.Target.GetSubject().Properties, err = structpb.NewStruct(map[string]any{"variant": property})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authorization.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: variant}); err != nil {
			t.Fatalf("add %s relationship variant = %v", property, err)
		}
	}
	read := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if len(read.Members) != 1 {
		t.Fatalf("physical variants projected members = %#v", read.Members)
	}
	if response = scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": read.DisplayName, "members": []map[string]any{{"value": user.ID}},
	}, map[string]string{"If-Match": read.Meta.Version}); response.Code != http.StatusOK {
		t.Fatalf("physical-variant no-op replacement = %d %s", response.Code, response.Body.String())
	}
	noop := decodeResponse[scim.Group](t, response)
	if noop.Meta.Version != read.Meta.Version {
		t.Fatalf("property-only no-op changed version from %q to %q", read.Meta.Version, noop.Meta.Version)
	}
	if response = scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": read.DisplayName, "members": []map[string]any{},
	}, map[string]string{"If-Match": noop.Meta.Version}); response.Code != http.StatusOK {
		t.Fatalf("remove physical-variant member = %d %s", response.Code, response.Body.String())
	}
	removed := decodeResponse[scim.Group](t, response)
	if len(removed.Members) != 0 {
		t.Fatalf("removed physical variants still projected members = %#v", removed.Members)
	}
	remaining, err := authorization.ListRelationships(context.Background(), &proto.ListRelationshipsRequest{Filter: &proto.RelationshipFilter{
		Resource: &proto.Resource{Type: "group", Id: group.ID}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Relationships) != 0 {
		t.Fatalf("physical variants remaining after HTTP removal = %#v", remaining.Relationships)
	}
}
