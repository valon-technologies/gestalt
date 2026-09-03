package scim_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/scim"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
)

type relationshipWriterMode int

const (
	relationshipWriterWorks relationshipWriterMode = iota
	relationshipWriterUnimplemented
	relationshipWriterFails
)

type batchRecordingAuthorization struct {
	*recordingAuthorization
	mode   relationshipWriterMode
	writes [][]*proto.RelationshipUpdate
}

func (a *batchRecordingAuthorization) WriteRelationships(_ context.Context, req *proto.WriteRelationshipsRequest) (*proto.WriteRelationshipsResponse, error) {
	a.mu.Lock()
	updates := make([]*proto.RelationshipUpdate, 0, len(req.GetUpdates()))
	for _, update := range req.GetUpdates() {
		updates = append(updates, gproto.Clone(update).(*proto.RelationshipUpdate))
	}
	a.writes = append(a.writes, updates)
	mode := a.mode
	a.mu.Unlock()
	if mode == relationshipWriterUnimplemented {
		return nil, status.Error(codes.Unimplemented, "writer unavailable")
	}
	if mode == relationshipWriterFails {
		return nil, errors.New("injected writer failure")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, update := range req.GetUpdates() {
		if update.GetOperation() == proto.RelationshipUpdate_OPERATION_DELETE {
			delete(a.relations, relationshipKey(update.GetRelationship().GetTuple()))
			continue
		}
		a.relations[relationshipKey(update.GetRelationship().GetTuple())] = gproto.Clone(update.GetRelationship()).(*proto.Relationship)
	}
	return &proto.WriteRelationshipsResponse{}, nil
}

func TestSCIMAtomicRelationshipBatchUsesOneWriter(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, nil, authorization, cfg)
	alice, response := createUser(t, handler, testCurrentToken, "batch-alice@valon.com", true, nil)
	if response.Code != 201 {
		t.Fatalf("create Alice = %d %s", response.Code, response.Body.String())
	}
	bob, response := createUser(t, handler, testCurrentToken, "batch-bob@valon.com", true, nil)
	if response.Code != 201 {
		t.Fatalf("create Bob = %d %s", response.Code, response.Body.String())
	}
	authorization.mu.Lock()
	writesBefore := len(authorization.writes)
	authorization.mu.Unlock()
	groupResponse := scimRequest(t, handler, "POST", "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Batch group", "members": []map[string]any{{"value": alice.ID}, {"value": bob.ID}},
	})
	if groupResponse.Code != 201 {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	authorization.mu.Lock()
	if len(authorization.writes) == writesBefore {
		authorization.mu.Unlock()
		t.Fatal("group batch did not invoke the relationship writer")
	}
	lastWrite := authorization.writes[len(authorization.writes)-1]
	writeCalls := len(authorization.writes) - writesBefore
	authorization.mu.Unlock()
	if writeCalls != 1 || len(lastWrite) != 2 {
		t.Fatalf("group batch writes = %d calls, %#v updates", writeCalls, lastWrite)
	}
	for _, update := range lastWrite {
		if update.GetRelationship().GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
			t.Fatalf("batch source layer = %v, want runtime", update.GetRelationship().GetSourceLayer())
		}
		if update.GetOperation() != proto.RelationshipUpdate_OPERATION_TOUCH {
			t.Fatalf("group create operation = %v, want TOUCH", update.GetOperation())
		}
	}
}

func TestSCIMRelationshipBatchOrderAndEmptyDiff(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, nil, authorization, cfg)
	alice, response := createUser(t, handler, testCurrentToken, "order-alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Alice = %d %s", response.Code, response.Body.String())
	}
	bob, response := createUser(t, handler, testCurrentToken, "order-bob@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Bob = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Order group", "members": []map[string]any{{"value": alice.ID}},
	})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create order group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	if response = scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": group.DisplayName, "members": []map[string]any{{"value": bob.ID}},
	}, map[string]string{"If-Match": group.Meta.Version}); response.Code != http.StatusOK {
		t.Fatalf("replace order group = %d %s", response.Code, response.Body.String())
	}
	authorization.mu.Lock()
	updates := authorization.writes[len(authorization.writes)-1]
	writes := len(authorization.writes)
	authorization.mu.Unlock()
	if len(updates) != 2 || updates[0].GetOperation() != proto.RelationshipUpdate_OPERATION_DELETE || updates[1].GetOperation() != proto.RelationshipUpdate_OPERATION_TOUCH {
		t.Fatalf("replacement updates = %#v, want DELETE then TOUCH", updates)
	}
	current := decodeResponse[scim.Group](t, response)
	if response = scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+current.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": current.DisplayName, "members": []map[string]any{{"value": bob.ID}},
	}, map[string]string{"If-Match": current.Meta.Version}); response.Code != http.StatusOK {
		t.Fatalf("empty-diff group replacement = %d %s", response.Code, response.Body.String())
	}
	authorization.mu.Lock()
	defer authorization.mu.Unlock()
	if len(authorization.writes) != writes {
		t.Fatalf("empty diff writer calls = %d, want %d", len(authorization.writes), writes)
	}
}

func TestSCIMRelationshipWriterFallsBackOnlyForUnimplemented(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mode       relationshipWriterMode
		wantStatus int
		wantAdds   int
	}{
		{name: "supported", mode: relationshipWriterWorks, wantStatus: http.StatusCreated},
		{name: "unimplemented", mode: relationshipWriterUnimplemented, wantStatus: http.StatusCreated, wantAdds: 1},
		{name: "other error", mode: relationshipWriterFails, wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization(), mode: test.mode}
			cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
			_, _, handler := newSCIMService(t, nil, authorization, cfg)
			user, response := createUser(t, handler, testCurrentToken, "fallback-"+test.name+"@valon.com", true, nil)
			if response.Code != http.StatusCreated {
				t.Fatalf("create user = %d %s", response.Code, response.Body.String())
			}
			groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
				"schemas": []string{scim.GroupSchemaURN}, "displayName": "Fallback " + test.name, "members": []map[string]any{{"value": user.ID}},
			})
			if groupResponse.Code != test.wantStatus {
				t.Fatalf("create group = %d %s, want %d", groupResponse.Code, groupResponse.Body.String(), test.wantStatus)
			}
			authorization.mu.Lock()
			writeCalls := len(authorization.writes)
			authorization.mu.Unlock()
			if writeCalls != 1 {
				t.Fatalf("writer calls = %d, want 1", writeCalls)
			}
			if got := authorization.additionCount(); got != test.wantAdds {
				t.Fatalf("legacy additions = %d, want %d", got, test.wantAdds)
			}
		})
	}
}

func TestSCIMLegacyFallbackTouchesSuccessfulPrefixOnFailure(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{
		recordingAuthorization: newRecordingAuthorization(),
		mode:                   relationshipWriterUnimplemented,
	}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, nil, authorization, cfg)
	alice, response := createUser(t, handler, testCurrentToken, "prefix-alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Alice = %d %s", response.Code, response.Body.String())
	}
	bob, response := createUser(t, handler, testCurrentToken, "prefix-bob@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Bob = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "Fallback prefix",
	})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create empty group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	before := map[string]scim.Meta{}
	for _, user := range []*scim.User{alice, bob} {
		read := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+user.ID, testCurrentToken, nil))
		before[user.ID] = read.Meta
	}
	authorization.setFailureAt(2, 0)
	authorization.mu.Lock()
	writesBefore := len(authorization.writes)
	authorization.mu.Unlock()

	replacement := scimRequest(t, handler, http.MethodPut, "/scim/v2/Groups/"+group.ID, testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": group.DisplayName, "members": []map[string]any{{"value": alice.ID}, {"value": bob.ID}},
	}, map[string]string{"If-Match": group.Meta.Version})
	if replacement.Code != http.StatusServiceUnavailable {
		t.Fatalf("partial fallback group replacement = %d %s, want 503", replacement.Code, replacement.Body.String())
	}
	authorization.mu.Lock()
	writeCalls := len(authorization.writes) - writesBefore
	authorization.mu.Unlock()
	if writeCalls != 1 {
		t.Fatalf("fallback writer calls = %d, want 1", writeCalls)
	}
	groupAfter := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	if groupAfter.Meta.Version == group.Meta.Version || len(groupAfter.Members) != 1 {
		t.Fatalf("successful legacy prefix group projection = %#v", groupAfter)
	}
	memberID := groupAfter.Members[0].Value
	memberAfter := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+memberID, testCurrentToken, nil))
	if !memberAfter.Meta.LastModified.After(before[memberID].LastModified) {
		t.Fatalf("successful legacy prefix did not advance affected user metadata = before %#v after %#v", before[memberID], memberAfter.Meta)
	}
	if len(memberAfter.Groups) != 1 || memberAfter.Groups[0].Value != group.ID {
		t.Fatalf("successful legacy prefix was not visible through the affected user = %#v", memberAfter.Groups)
	}
}

func TestSCIMSupportedRelationshipWriterDowngradesAfterUnimplemented(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization(), mode: relationshipWriterWorks}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	_, _, handler := newSCIMService(t, nil, authorization, cfg)
	user, response := createUser(t, handler, testCurrentToken, "downgrade@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", response.Code, response.Body.String())
	}
	createGroup := func(name string) {
		t.Helper()
		response := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
			"schemas": []string{scim.GroupSchemaURN}, "displayName": name, "members": []map[string]any{{"value": user.ID}},
		})
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", name, response.Code, response.Body.String())
		}
	}
	createGroup("Supported")
	authorization.mu.Lock()
	authorization.mode = relationshipWriterUnimplemented
	authorization.mu.Unlock()
	createGroup("Downgrade")
	authorization.mu.Lock()
	writerCalls := len(authorization.writes)
	authorization.mu.Unlock()
	if writerCalls != 2 {
		t.Fatalf("writer calls after downgrade = %d, want 2", writerCalls)
	}
	createGroup("Cached fallback")
	authorization.mu.Lock()
	writerCalls = len(authorization.writes)
	authorization.mu.Unlock()
	if writerCalls != 2 {
		t.Fatalf("cached unsupported writer calls = %d, want 2", writerCalls)
	}
	if got := authorization.additionCount(); got != 2 {
		t.Fatalf("legacy additions after downgrade = %d, want 2", got)
	}
}

func TestSCIMExternalRelationshipBatchUsesOneWriterAndUpdatesSCIMResources(t *testing.T) {
	t.Parallel()

	authorization := &batchRecordingAuthorization{recordingAuthorization: newRecordingAuthorization()}
	cfg := testSCIMConfig(map[string]config.SCIMClientConfig{"rippling": ripplingClient(nil)})
	service, services, handler := newSCIMService(t, nil, authorization, cfg)
	alice, response := createUser(t, handler, testCurrentToken, "external-batch-alice@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Alice = %d %s", response.Code, response.Body.String())
	}
	bob, response := createUser(t, handler, testCurrentToken, "external-batch-bob@valon.com", true, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create Bob = %d %s", response.Code, response.Body.String())
	}
	groupResponse := scimRequest(t, handler, http.MethodPost, "/scim/v2/Groups", testCurrentToken, map[string]any{
		"schemas": []string{scim.GroupSchemaURN}, "displayName": "External batch",
	})
	group := decodeResponse[scim.Group](t, groupResponse)
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("create group = %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	groupBefore := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	aliceCore, err := services.Users.FindUserByEmail(context.Background(), "external-batch-alice@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	bobCore, err := services.Users.FindUserByEmail(context.Background(), "external-batch-bob@valon.com")
	if err != nil {
		t.Fatal(err)
	}
	aliceBefore := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+alice.ID, testCurrentToken, nil))
	bobBefore := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+bob.ID, testCurrentToken, nil))
	updates := []*proto.RelationshipUpdate{
		{Operation: proto.RelationshipUpdate_OPERATION_TOUCH, Relationship: runtimeGroupMember(aliceCore.ID, group.ID)},
		{Operation: proto.RelationshipUpdate_OPERATION_TOUCH, Relationship: runtimeGroupMember(bobCore.ID, group.ID)},
	}
	authorization.mu.Lock()
	writesBefore := len(authorization.writes)
	authorization.mu.Unlock()
	gate, ok := scim.WrapAuthorization(authorization, services.Users, service).(core.AuthorizationRelationshipWriter)
	if !ok {
		t.Fatal("wrapped authorization does not implement relationship writer")
	}
	if _, err := gate.WriteRelationships(context.Background(), &proto.WriteRelationshipsRequest{Updates: updates}); err != nil {
		t.Fatalf("external WriteRelationships = %v", err)
	}
	authorization.mu.Lock()
	writeCalls := len(authorization.writes) - writesBefore
	authorization.mu.Unlock()
	if writeCalls != 1 {
		t.Fatalf("external writer calls = %d, want 1", writeCalls)
	}
	groupAfter := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	aliceAfter := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+alice.ID, testCurrentToken, nil))
	bobAfter := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+bob.ID, testCurrentToken, nil))
	if len(groupAfter.Members) != 2 || groupAfter.Meta.Version == groupBefore.Meta.Version || !groupAfter.Meta.LastModified.After(groupBefore.Meta.LastModified) {
		t.Fatalf("external group membership was not reflected = before %#v after %#v", groupBefore, groupAfter)
	}
	if len(aliceAfter.Groups) != 1 || aliceAfter.Groups[0].Value != group.ID || aliceAfter.Meta.Version == aliceBefore.Meta.Version || !aliceAfter.Meta.LastModified.After(aliceBefore.Meta.LastModified) {
		t.Fatalf("external Alice membership was not reflected = before %#v after %#v", aliceBefore, aliceAfter)
	}
	if len(bobAfter.Groups) != 1 || bobAfter.Groups[0].Value != group.ID || bobAfter.Meta.Version == bobBefore.Meta.Version || !bobAfter.Meta.LastModified.After(bobBefore.Meta.LastModified) {
		t.Fatalf("external Bob membership was not reflected = before %#v after %#v", bobBefore, bobAfter)
	}

	unspecifiedDelete := runtimeGroupMember(aliceCore.ID, group.ID)
	unspecifiedDelete.SourceLayer = proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED
	if _, err := gate.WriteRelationships(context.Background(), &proto.WriteRelationshipsRequest{Updates: []*proto.RelationshipUpdate{{
		Operation:    proto.RelationshipUpdate_OPERATION_DELETE,
		Relationship: unspecifiedDelete,
	}}}); err != nil {
		t.Fatalf("external unspecified-source delete = %v", err)
	}
	groupAfterDelete := decodeResponse[scim.Group](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Groups/"+group.ID, testCurrentToken, nil))
	aliceAfterDelete := decodeResponse[scim.User](t, scimRequest(t, handler, http.MethodGet, "/scim/v2/Users/"+alice.ID, testCurrentToken, nil))
	if len(groupAfterDelete.Members) != 1 || groupAfterDelete.Members[0].Value != bob.ID || groupAfterDelete.Meta.Version == groupAfter.Meta.Version || !groupAfterDelete.Meta.LastModified.After(groupAfter.Meta.LastModified) {
		t.Fatalf("unspecified-source delete was not reflected in group = before %#v after %#v", groupAfter, groupAfterDelete)
	}
	if len(aliceAfterDelete.Groups) != 0 || aliceAfterDelete.Meta.Version == aliceAfter.Meta.Version || !aliceAfterDelete.Meta.LastModified.After(aliceAfter.Meta.LastModified) {
		t.Fatalf("unspecified-source delete was not reflected for Alice = before %#v after %#v", aliceAfter, aliceAfterDelete)
	}
}

func runtimeGroupMember(coreID, groupID string) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target:   &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + coreID}}},
			Relation: "member",
			Resource: &proto.Resource{Type: "group", Id: groupID},
		},
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
	}
}
