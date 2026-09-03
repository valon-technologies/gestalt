package scim_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestSCIMGroupPatchMembershipOperations(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	clients := map[string]config.SCIMClientConfig{
		"rippling": ripplingClient(nil),
		"entra":    {Credentials: []config.SCIMCredentialConfig{{ID: "current", BearerToken: "entra-token"}}},
	}
	_, _, handler := newSCIMService(t, nil, authorization, testSCIMConfig(clients))
	alice, response := createUser(t, handler, testCurrentToken, "patch-alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Alice = %d %s", response.Code, response.Body.String())
	}
	bob, response := createUser(t, handler, testCurrentToken, "patch-bob@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Bob = %d %s", response.Code, response.Body.String())
	}
	charlie, response := createUser(t, handler, testCurrentToken, "patch-charlie@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Charlie = %d %s", response.Code, response.Body.String())
	}
	foreign, response := createUser(t, handler, "entra-token", "patch-foreign@example.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create foreign user = %d %s", response.Code, response.Body.String())
	}
	createdResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Patch group",
	})
	group := decodeResponse[scim.Group](t, createdResponse)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", createdResponse.Code, createdResponse.Body.String())
	}
	unrelated := &proto.Relationship{Tuple: &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:unrelated"}}}, Relation: "viewer", Resource: &proto.Resource{Type: "app", Id: "unrelated"}}, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}
	if _, err := authorization.AddRelationship(context.Background(), &proto.AddRelationshipRequest{Relationship: unrelated}); err != nil {
		t.Fatalf("seed unrelated relationship = %v", err)
	}

	add := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID+"?attributes=members", testCurrentToken, map[string]any{
		"schemas":    []string{scim.PatchOpSchemaURN},
		"Operations": []map[string]any{{"op": "ADD", "path": " members ", "value": []map[string]any{{"value": alice.ID, "type": "User", "display": "ignored"}, {"value": bob.ID}}}},
	}, map[string]string{"If-Match": group.Meta.Version})
	if add.Code != http.StatusOK {
		t.Fatalf("add members = %d %s", add.Code, add.Body.String())
	}
	group = decodeResponse[scim.Group](t, add)
	if len(group.Members) != 2 || add.Header().Get("Location") == "" || add.Header().Get("Content-Location") == "" || add.Header().Get("ETag") == "" {
		t.Fatalf("add response = %d headers=%v body=%#v", add.Code, add.Header(), group)
	}
	if strings.Contains(add.Body.String(), "displayName") {
		t.Fatalf("PATCH attributes projection retained displayName: %s", add.Body.String())
	}
	immediate := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if add.Header().Get("Location") != immediate.Meta.Location || add.Header().Get("Content-Location") != immediate.Meta.Location || add.Header().Get("ETag") != immediate.Meta.Version || !immediate.Meta.LastModified.After(immediate.Meta.Created) || len(immediate.Members) != len(group.Members) {
		t.Fatalf("PATCH response differs from immediate GET = response %#v get %#v", group, immediate)
	}
	group = immediate
	authorization.mu.Lock()
	if len(authorization.writes) == 0 || len(authorization.writes[len(authorization.writes)-1]) != 2 {
		authorization.mu.Unlock()
		t.Fatal("add did not issue one two-tuple atomic write")
	}
	writesAfterAdd := len(authorization.writes)
	authorization.mu.Unlock()

	duplicate := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": alice.ID}}},
	}, map[string]string{"If-Match": group.Meta.Version})
	duplicateGroup := decodeResponse[scim.Group](t, duplicate)
	if duplicate.Code != http.StatusOK || duplicateGroup.Meta.Version != group.Meta.Version || !duplicateGroup.Meta.LastModified.Equal(group.Meta.LastModified) {
		t.Fatalf("duplicate add changed representation = %d %#v (before %#v)", duplicate.Code, duplicateGroup, group)
	}
	authorization.mu.Lock()
	if len(authorization.writes) != writesAfterAdd {
		authorization.mu.Unlock()
		t.Fatal("duplicate add issued an atomic write")
	}
	authorization.mu.Unlock()

	qualifiedRemove := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": " ReMoVe ", "path": "  " + scim.GroupSchemaURN + ":members [ value EQ \"" + strings.ReplaceAll(alice.ID, "-", "\\u002d") + "\" ] "}},
	}, map[string]string{"If-Match": group.Meta.Version})
	if qualifiedRemove.Code != http.StatusOK {
		t.Fatalf("qualified filtered remove = %d %s", qualifiedRemove.Code, qualifiedRemove.Body.String())
	}
	group = decodeResponse[scim.Group](t, qualifiedRemove)
	if len(group.Members) != 1 || group.Members[0].Value != bob.ID {
		t.Fatalf("qualified filtered remove members = %#v", group.Members)
	}
	authorization.mu.Lock()
	writesBeforeAbsentRemove := len(authorization.writes)
	authorization.mu.Unlock()
	absentRemove := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members[value eq \"" + alice.ID + "\"]"}},
	}, map[string]string{"If-Match": group.Meta.Version})
	absentGroup := decodeResponse[scim.Group](t, absentRemove)
	if absentRemove.Code != http.StatusOK || absentGroup.Meta.Version != group.Meta.Version || !absentGroup.Meta.LastModified.Equal(group.Meta.LastModified) {
		t.Fatalf("already-absent removal = %d %#v (before %#v)", absentRemove.Code, absentGroup, group)
	}
	authorization.mu.Lock()
	if len(authorization.writes) != writesBeforeAbsentRemove {
		authorization.mu.Unlock()
		t.Fatal("already-absent removal issued an atomic write")
	}
	authorization.mu.Unlock()

	pathlessAdd := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"SCHEMAS": []string{scim.PatchOpSchemaURN}, "OPERATIONS": []map[string]any{{"OP": "add", "VALUE": map[string]any{"MEMBERS": map[string]any{"VALUE": alice.ID}}}},
	}, map[string]string{"If-Match": group.Meta.Version})
	if pathlessAdd.Code != http.StatusOK || len(decodeResponse[scim.Group](t, pathlessAdd).Members) != 2 {
		t.Fatalf("pathless add = %d %s", pathlessAdd.Code, pathlessAdd.Body.String())
	}
	group = decodeResponse[scim.Group](t, pathlessAdd)

	authorization.mu.Lock()
	writesBeforeOrdering := len(authorization.writes)
	addCallsBeforeOrdering, deleteCallsBeforeOrdering := authorization.addCalls, authorization.deleteCalls
	authorization.mu.Unlock()
	ordering := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{
			{"op": "remove", "path": "members[value eq \"" + bob.ID + "\"]"},
			{"op": "add", "path": "members", "value": map[string]any{"value": charlie.ID}},
		},
	}, map[string]string{"If-Match": group.Meta.Version})
	ordered := decodeResponse[scim.Group](t, ordering)
	if ordering.Code != http.StatusOK || ordered.Meta.Version == group.Meta.Version || len(ordered.Members) != 2 {
		t.Fatalf("remove/add ordering = %d %#v", ordering.Code, ordered)
	}
	authorization.mu.Lock()
	if len(authorization.writes) != writesBeforeOrdering+1 || authorization.addCalls != addCallsBeforeOrdering || authorization.deleteCalls != deleteCallsBeforeOrdering {
		authorization.mu.Unlock()
		t.Fatalf("PATCH used unexpected relationship calls: writes=%d adds=%d deletes=%d", len(authorization.writes), authorization.addCalls, authorization.deleteCalls)
	}
	authorization.mu.Unlock()
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members[value eq \"" + bob.ID + "\"]"}},
	}, map[string]string{"If-Match": "W/\"stale\""}); response.Code != http.StatusPreconditionFailed || decodeResponse[testErrorResponse](t, response).Status != "412" {
		t.Fatalf("stale If-Match = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": foreign.ID}}},
	}); response.Code != http.StatusBadRequest || decodeResponse[testErrorResponse](t, response).SCIMType != "noTarget" {
		t.Fatalf("foreign member namespace = %d %s", response.Code, response.Body.String())
	}
	if response := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members[value eq \"missing\"]"}},
	}); response.Code != http.StatusBadRequest || decodeResponse[testErrorResponse](t, response).SCIMType != "noTarget" {
		t.Fatalf("missing member removal = %d %s", response.Code, response.Body.String())
	}
	foreignGroupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", "entra-token", map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Foreign patch group"})
	foreignGroup := decodeResponse[scim.Group](t, foreignGroupResponse)
	if foreignGroupResponse.Code != http.StatusCreated {
		t.Fatalf("create foreign Group = %d %s", foreignGroupResponse.Code, foreignGroupResponse.Body.String())
	}
	foreignRead := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+foreignGroup.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": bob.ID}}},
	})
	if foreignRead.Code != http.StatusNotFound || decodeResponse[testErrorResponse](t, foreignRead).Status != "404" {
		t.Fatalf("foreign Group PATCH = %d %s", foreignRead.Code, foreignRead.Body.String())
	}
	missingRead := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/missing-group", testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": bob.ID}}},
	})
	if missingRead.Code != http.StatusNotFound || decodeResponse[testErrorResponse](t, missingRead).Status != "404" {
		t.Fatalf("missing Group PATCH = %d %s", missingRead.Code, missingRead.Body.String())
	}
	authorization.mu.Lock()
	if _, ok := authorization.relations[relationshipKey(unrelated.Tuple)]; !ok {
		authorization.mu.Unlock()
		t.Fatal("PATCH removed an unrelated runtime relationship")
	}
	authorization.mu.Unlock()
}

func TestSCIMGroupPatchValidationAndAtomicFailures(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	_, _, handler := newSCIMService(t, nil, authorization, testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)}))
	user, response := createUser(t, handler, testCurrentToken, "patch-validation@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Patch validation"})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	tests := []struct {
		name string
		body map[string]any
		code int
		typ  string
	}{
		{"missing schema", map[string]any{"Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}}}}, 400, "invalidSyntax"},
		{"unknown path", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "unknown", "value": map[string]any{"value": user.ID}}}}, 400, "invalidPath"},
		{"immutable subattribute", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members.value", "value": user.ID}}}, 400, "mutability"},
		{"unsupported replace", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "members", "value": []map[string]any{{"value": user.ID}}}}}, 501, ""},
		{"unsupported remove all", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members"}}}, 501, ""},
		{"metadata path", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "schemas", "value": []string{scim.GroupSchemaURN}}}}, 501, ""},
		{"metadata path missing value", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "schemas"}}}, 400, "invalidValue"},
		{"metadata in pathless value", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "value": map[string]any{"members": []map[string]any{}, "displayName": "renamed"}}}}, 501, ""},
		{"replace missing value", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "replace", "path": "members"}}}, 400, "invalidValue"},
		{"immutable filtered display", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members[value eq \"" + user.ID + "\"].display"}}}, 400, "mutability"},
		{"mismatched type", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID, "type": "Group"}}}}, 400, "invalidValue"},
		{"mismatched ref", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID, "$ref": "https://wrong.example/Users/" + user.ID}}}}, 400, "invalidValue"},
		{"unknown member", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": "missing-user"}}}}, 400, "noTarget"},
		{"unknown member subattribute", map[string]any{"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members.bogus", "value": user.ID}}}, 400, "invalidPath"},
	}
	for _, test := range tests { //nolint:paralleltest // these cases share the provider to verify zero writes
		t.Run(test.name, func(t *testing.T) {
			authorization.mu.Lock()
			writesBefore := len(authorization.writes)
			authorization.mu.Unlock()
			result := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, test.body)
			payload := decodeResponse[testErrorResponse](t, result)
			if result.Code != test.code || test.typ != "" && payload.SCIMType != test.typ {
				t.Fatalf("result = %d %#v, want %d/%s", result.Code, payload, test.code, test.typ)
			}
			authorization.mu.Lock()
			writesAfter := len(authorization.writes)
			authorization.mu.Unlock()
			if writesAfter != writesBefore {
				t.Fatalf("invalid/unsupported request issued a provider write: %d -> %d", writesBefore, writesAfter)
			}
		})
	}
	authorization.mu.Lock()
	writesBeforePrevalidation := len(authorization.writes)
	authorization.mu.Unlock()
	prevalidation := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{
			{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}},
			{"op": "replace", "path": "members", "value": []map[string]any{{"value": user.ID}}},
		},
	})
	if prevalidation.Code != http.StatusNotImplemented {
		t.Fatalf("valid operation followed by unsupported operation = %d %s", prevalidation.Code, prevalidation.Body.String())
	}
	authorization.mu.Lock()
	if len(authorization.writes) != writesBeforePrevalidation {
		authorization.mu.Unlock()
		t.Fatal("request-wide prevalidation issued a provider write")
	}
	authorization.mu.Unlock()
	authorization.mu.Lock()
	writesBeforeFailure := len(authorization.writes)
	authorization.mode = relationshipWriterFails
	authorization.mu.Unlock()
	failed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}}},
	})
	failedPayload := decodeResponse[testErrorResponse](t, failed)
	if failed.Code != http.StatusServiceUnavailable || failed.Header().Get("Retry-After") != "1" || failedPayload.Status != "503" || len(failedPayload.Schemas) != 1 || failedPayload.Schemas[0] != scim.ErrorSchemaURN {
		t.Fatalf("transient writer failure = %d headers=%v body=%s", failed.Code, failed.Header(), failed.Body.String())
	}
	if got := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil)); len(got.Members) != 0 {
		t.Fatalf("failed atomic patch changed membership = %#v", got.Members)
	}
	authorization.mu.Lock()
	if len(authorization.writes) != writesBeforeFailure+1 {
		t.Fatalf("writer calls after transient failure = %d, want %d", len(authorization.writes), writesBeforeFailure+1)
	}
	authorization.mode = relationshipWriterUnimplemented
	authorization.mu.Unlock()
	unimplemented := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}}},
	})
	unimplementedPayload := decodeResponse[testErrorResponse](t, unimplemented)
	if unimplemented.Code != http.StatusNotImplemented || unimplementedPayload.Status != "501" || len(unimplementedPayload.Schemas) != 1 || unimplementedPayload.Schemas[0] != scim.ErrorSchemaURN {
		t.Fatalf("unimplemented writer = %d %s", unimplemented.Code, unimplemented.Body.String())
	}
	noWriter := newRecordingAuthorization()
	_, _, noWriterHandler := newSCIMService(t, nil, noWriter, testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)}))
	noWriterUser, response := createUser(t, noWriterHandler, testCurrentToken, "patch-no-writer@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create no-writer user = %d %s", response.Code, response.Body.String())
	}
	noWriterGroupResponse := scimRequest(t, noWriterHandler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "No writer"})
	noWriterGroup := decodeResponse[scim.Group](t, noWriterGroupResponse)
	noWriterPatch := scimRequest(t, noWriterHandler, http.MethodPatch, "/scim/v2/Groups/"+noWriterGroup.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": noWriterUser.ID}}},
	})
	if noWriterPatch.Code != http.StatusNotImplemented || decodeResponse[testErrorResponse](t, noWriterPatch).Status != "501" {
		t.Fatalf("missing atomic writer = %d %s", noWriterPatch.Code, noWriterPatch.Body.String())
	}
	if noWriter.additionCount() != 0 {
		t.Fatalf("missing atomic writer fell back to AddRelationship: %d", noWriter.additionCount())
	}
}

func TestSCIMGroupPatchNestedGroupMembers(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	_, _, handler := newSCIMService(t, nil, authorization, testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)}))
	user, response := createUser(t, handler, testCurrentToken, "patch-nested@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	childResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Patch child"})
	child := decodeResponse[scim.Group](t, childResponse)
	parentResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{"schemas": []string{scim.GroupSchemaURN}, "displayName": "Patch parent"})
	parent := decodeResponse[scim.Group](t, parentResponse)
	if childResponse.Code != http.StatusCreated || parentResponse.Code != http.StatusCreated {
		t.Fatalf("create nested groups = child %d/%s parent %d/%s", childResponse.Code, childResponse.Body.String(), parentResponse.Code, parentResponse.Body.String())
	}
	added := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+parent.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": child.ID, "type": "Group"}}},
	}, map[string]string{"If-Match": parent.Meta.Version})
	parent = decodeResponse[scim.Group](t, added)
	if added.Code != http.StatusOK || len(parent.Members) != 1 || parent.Members[0].Value != child.ID || parent.Members[0].Type != "Group" {
		t.Fatalf("nested Group add = %d %#v", added.Code, parent)
	}
	child = decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+child.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "add", "path": "members", "value": map[string]any{"value": user.ID}}},
	}, map[string]string{"If-Match": child.Meta.Version}))
	if len(child.Members) != 1 || child.Members[0].Value != user.ID {
		t.Fatalf("nested child member = %#v", child.Members)
	}
	removed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+parent.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members[value eq \"" + child.ID + "\"]"}},
	}, map[string]string{"If-Match": parent.Meta.Version})
	if removed.Code != http.StatusOK || len(decodeResponse[scim.Group](t, removed).Members) != 0 {
		t.Fatalf("nested Group remove = %d %s", removed.Code, removed.Body.String())
	}
}

func TestSCIMGroupPatchRemovesAllPhysicalMemberVariants(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	_, services, handler := newSCIMService(t, nil, authorization, testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)}))
	user, response := createUser(t, handler, testCurrentToken, "patch-physical-variants@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Patch physical variants", "members": []map[string]any{{"value": user.ID}},
	})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	coreUser, err := services.Users.FindUserByEmail(context.Background(), "patch-physical-variants@valon.com")
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
	authorization.mu.Lock()
	writesBefore := len(authorization.writes)
	authorization.mu.Unlock()
	removed := scimRequest(t, handler, http.MethodPatch, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.PatchOpSchemaURN}, "Operations": []map[string]any{{"op": "remove", "path": "members[value eq \"" + user.ID + "\"]"}},
	}, map[string]string{"If-Match": read.Meta.Version})
	if removed.Code != http.StatusOK {
		t.Fatalf("remove physical variants = %d %s", removed.Code, removed.Body.String())
	}
	if got := decodeResponse[scim.Group](t, removed); len(got.Members) != 0 {
		t.Fatalf("PATCH response retained physical variants = %#v", got.Members)
	}
	immediate := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if len(immediate.Members) != 0 {
		t.Fatalf("immediate GET retained physical variants = %#v", immediate.Members)
	}
	remaining, err := authorization.ListRelationships(context.Background(), &proto.ListRelationshipsRequest{Filter: &proto.RelationshipFilter{
		Resource: &proto.Resource{Type: "group", Id: group.ID}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Relationships) != 0 {
		t.Fatalf("physical variants remaining after PATCH = %#v", remaining.Relationships)
	}
	authorization.mu.Lock()
	defer authorization.mu.Unlock()
	if len(authorization.writes) != writesBefore+1 || len(authorization.writes[len(authorization.writes)-1]) != 3 {
		t.Fatalf("physical variant PATCH writer calls = %d updates=%#v", len(authorization.writes)-writesBefore, authorization.writes[len(authorization.writes)-1])
	}
	for _, update := range authorization.writes[len(authorization.writes)-1] {
		if update.GetOperation() != proto.RelationshipUpdate_OPERATION_DELETE {
			t.Fatalf("physical variant PATCH operation = %v, want DELETE", update.GetOperation())
		}
	}
}
