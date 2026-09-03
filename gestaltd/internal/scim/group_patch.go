package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const PatchOpSchemaURN = "urn:ietf:params:scim:api:messages:2.0:PatchOp"

type groupPatchRequest struct {
	operations []groupPatchOperation
}

type groupPatchOperation struct {
	op      string
	path    groupPatchPath
	members []Member
}

type groupPatchPath struct {
	kind     groupPatchPathKind
	memberID string
}

type groupPatchPathKind int

const (
	groupPatchPathless groupPatchPathKind = iota
	groupPatchMembers
	groupPatchFilteredMember
	groupPatchSubattribute
	groupPatchMetadata
)

var memberFilterExpression = regexp.MustCompile(`(?is)^\s*value\s+eq\s+("(?:\\.|[^"\\])*")\s*$`)

// parseGroupPatchRequest validates the PatchOp envelope and all operation
// shapes that can be understood without consulting the SCIM resource store.
// Resource/member existence and $ref/type validation happen while evaluating
// the request under the per-group lock.
func parseGroupPatchRequest(raw json.RawMessage) (groupPatchRequest, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope == nil {
		return groupPatchRequest{}, invalidSyntax("PatchOp must be a JSON object")
	}
	schemasRaw, ok := rawField(envelope, "schemas")
	if !ok {
		return groupPatchRequest{}, invalidSyntax("PatchOp schemas is required")
	}
	var schemas []string
	if err := json.Unmarshal(schemasRaw, &schemas); err != nil || len(schemas) == 0 {
		return groupPatchRequest{}, invalidSyntax("PatchOp schemas must be a non-empty array")
	}
	patchSchemaCount := 0
	for _, schema := range schemas {
		if strings.TrimSpace(schema) == "" {
			return groupPatchRequest{}, invalidSyntax("PatchOp schemas must not contain empty values")
		}
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(schema)), "urn:") {
			return groupPatchRequest{}, invalidSyntax("PatchOp schemas must contain URNs")
		}
		if schema == PatchOpSchemaURN {
			patchSchemaCount++
		}
	}
	if patchSchemaCount != 1 {
		return groupPatchRequest{}, invalidSyntax("PatchOp schemas must contain exactly one PatchOp schema")
	}
	operationsRaw, ok := rawField(envelope, "Operations")
	if !ok {
		return groupPatchRequest{}, invalidSyntax("PatchOp Operations is required")
	}
	var operationValues []json.RawMessage
	if err := json.Unmarshal(operationsRaw, &operationValues); err != nil || len(operationValues) == 0 {
		return groupPatchRequest{}, invalidSyntax("PatchOp Operations must be a non-empty array")
	}
	request := groupPatchRequest{operations: make([]groupPatchOperation, 0, len(operationValues))}
	for _, operationRaw := range operationValues {
		operation, err := parseGroupPatchOperation(operationRaw)
		if err != nil {
			return groupPatchRequest{}, err
		}
		request.operations = append(request.operations, operation)
	}
	return request, nil
}

func parseGroupPatchOperation(raw json.RawMessage) (groupPatchOperation, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return groupPatchOperation{}, invalid("each PatchOp operation must be an object")
	}
	opRaw, ok := rawField(object, "op")
	if !ok {
		return groupPatchOperation{}, invalid("PatchOp operation op is required")
	}
	var op string
	if err := json.Unmarshal(opRaw, &op); err != nil || strings.TrimSpace(op) == "" {
		return groupPatchOperation{}, invalid("PatchOp operation op must be a string")
	}
	op = strings.ToLower(strings.TrimSpace(op))
	pathRaw, hasPath := rawField(object, "path")
	var pathText string
	if hasPath {
		if string(bytes.TrimSpace(pathRaw)) == "null" {
			return groupPatchOperation{}, invalid("PatchOp operation path must be a string")
		}
		if err := json.Unmarshal(pathRaw, &pathText); err != nil {
			return groupPatchOperation{}, invalid("PatchOp operation path must be a string")
		}
	}
	valueRaw, hasValue := rawField(object, "value")
	if op != "add" && op != "remove" && op != "replace" {
		return groupPatchOperation{}, invalid(fmt.Sprintf("unsupported PatchOp operation %q", op))
	}
	if (op == "add" || op == "replace") && !hasValue {
		return groupPatchOperation{}, invalid("PatchOp add and replace operations require value")
	}
	path, err := parseGroupPatchPath(pathText, hasPath)
	if err != nil {
		return groupPatchOperation{}, err
	}
	operation := groupPatchOperation{op: op, path: path}
	if path.kind == groupPatchSubattribute {
		return groupPatchOperation{}, mutability("group member subattributes are immutable or read-only")
	}
	if path.kind == groupPatchMetadata {
		return groupPatchOperation{}, notImplementedPatch("Group metadata PATCH is not supported")
	}
	if op == "replace" {
		return groupPatchOperation{}, notImplementedPatch("Group PATCH replace is not supported")
	}
	if op == "remove" {
		if !hasPath || path.kind == groupPatchPathless {
			return groupPatchOperation{}, noTarget("remove requires a member path")
		}
		if path.kind == groupPatchMembers {
			return groupPatchOperation{}, notImplementedPatch("removing the entire members collection is not supported")
		}
		return operation, nil
	}
	if !hasValue {
		return groupPatchOperation{}, invalid("add requires a value")
	}
	if path.kind == groupPatchFilteredMember {
		return groupPatchOperation{}, notImplementedPatch("filtered member add is not supported")
	}
	if path.kind == groupPatchPathless {
		var objectValue map[string]json.RawMessage
		if err := json.Unmarshal(valueRaw, &objectValue); err != nil || objectValue == nil {
			return groupPatchOperation{}, invalid("pathless add value must contain members")
		}
		for key := range objectValue {
			if strings.EqualFold(key, "displayName") || strings.EqualFold(key, "externalId") || strings.EqualFold(key, "schemas") {
				return groupPatchOperation{}, notImplementedPatch("Group metadata PATCH is not supported")
			}
		}
		membersRaw, ok := rawField(objectValue, "members")
		if !ok || len(objectValue) != 1 {
			return groupPatchOperation{}, invalid("pathless add value must contain only members")
		}
		members, err := parsePatchMembers(membersRaw)
		if err != nil {
			return groupPatchOperation{}, err
		}
		operation.members = members
		return operation, nil
	}
	members, err := parsePatchMembers(valueRaw)
	if err != nil {
		return groupPatchOperation{}, err
	}
	operation.members = members
	return operation, nil
}

func rawField(object map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	if value, ok := object[name]; ok {
		return value, true
	}
	for key, value := range object {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return nil, false
}

func parsePatchMembers(raw json.RawMessage) ([]Member, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, invalid("member value is required")
	}
	var values []json.RawMessage
	switch raw[0] {
	case '[':
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, invalid("members value must be an array or object")
		}
	case '{':
		values = []json.RawMessage{raw}
	default:
		return nil, invalid("members value must be an array or object")
	}
	if len(values) == 0 {
		return []Member{}, nil
	}
	result := make([]Member, 0, len(values))
	for _, value := range values {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(value, &object); err != nil || object == nil {
			return nil, invalid("each group member must be an object")
		}
		for key := range object {
			switch strings.ToLower(key) {
			case "value", "$ref", "type", "display":
			default:
				return nil, invalid("unsupported group member attribute")
			}
		}
		valueRaw, ok := rawField(object, "value")
		if !ok {
			return nil, invalid("group member value is required")
		}
		var id string
		if err := json.Unmarshal(valueRaw, &id); err != nil || strings.TrimSpace(id) == "" {
			return nil, invalid("group member value must be a non-empty string")
		}
		member := Member{Value: id}
		for _, field := range []struct {
			name        string
			destination *string
		}{{"$ref", &member.Ref}, {"type", &member.Type}, {"display", &member.Display}} {
			fieldRaw, present := rawField(object, field.name)
			if !present {
				continue
			}
			if string(bytes.TrimSpace(fieldRaw)) == "null" {
				return nil, invalid(fmt.Sprintf("group member %s must be a string", field.name))
			}
			if err := json.Unmarshal(fieldRaw, field.destination); err != nil {
				return nil, invalid(fmt.Sprintf("group member %s must be a string", field.name))
			}
		}
		result = append(result, member)
	}
	return result, nil
}

func parseGroupPatchPath(path string, supplied bool) (groupPatchPath, error) {
	path = strings.TrimSpace(path)
	if !supplied || path == "" {
		return groupPatchPath{kind: groupPatchPathless}, nil
	}
	lower := strings.ToLower(path)
	base := lower
	if index := strings.Index(base, "["); index >= 0 {
		close := strings.LastIndex(path, "]")
		if close < 0 {
			return groupPatchPath{}, invalidPath("malformed group member filter")
		}
		base = strings.TrimSpace(base[:index])
		if !isMembersPathBase(base) {
			return groupPatchPath{}, invalidPath("unknown Group PATCH path")
		}
		expression := path[index+1 : close]
		match := memberFilterExpression.FindStringSubmatch(expression)
		if len(match) != 2 {
			return groupPatchPath{}, invalidPath("malformed group member filter")
		}
		var id string
		if err := json.Unmarshal([]byte(match[1]), &id); err != nil {
			return groupPatchPath{}, invalidPath("malformed group member filter value")
		}
		if trailing := strings.TrimSpace(path[close+1:]); trailing != "" {
			if strings.HasPrefix(trailing, ".") {
				if isMemberSubattribute(strings.TrimPrefix(strings.ToLower(trailing), ".")) {
					return groupPatchPath{kind: groupPatchSubattribute}, nil
				}
				return groupPatchPath{}, invalidPath("unknown Group member subattribute")
			}
			return groupPatchPath{}, invalidPath("malformed group member filter")
		}
		return groupPatchPath{kind: groupPatchFilteredMember, memberID: id}, nil
	}
	if strings.Contains(lower, ".") {
		for _, prefix := range []string{"members.", strings.ToLower(GroupSchemaURN) + ":members."} {
			if strings.HasPrefix(lower, prefix) {
				if isMemberSubattribute(strings.TrimPrefix(lower, prefix)) {
					return groupPatchPath{kind: groupPatchSubattribute}, nil
				}
				return groupPatchPath{}, invalidPath("unknown Group member subattribute")
			}
		}
	}
	if isMembersPathBase(lower) {
		return groupPatchPath{kind: groupPatchMembers}, nil
	}
	if lower == "displayname" || lower == "externalid" || lower == "schemas" || lower == strings.ToLower(GroupSchemaURN)+":displayname" || lower == strings.ToLower(GroupSchemaURN)+":externalid" || lower == strings.ToLower(GroupSchemaURN)+":schemas" {
		return groupPatchPath{kind: groupPatchMetadata}, nil
	}
	return groupPatchPath{}, invalidPath("unknown Group PATCH path")
}

func isMembersPathBase(path string) bool {
	return path == "members" || path == strings.ToLower(GroupSchemaURN)+":members"
}

func isMemberSubattribute(path string) bool {
	switch strings.TrimSpace(path) {
	case "value", "$ref", "type", "display":
		return true
	default:
		return false
	}
}

func invalidPath(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "invalidPath", Detail: detail}
}

func notImplementedPatch(detail string) *Error {
	return &Error{Status: http.StatusNotImplemented, Detail: detail}
}

type patchMemberState struct {
	member Member
	tuple  *proto.RelationshipTuple
}

func (s *CompactService) PatchGroup(ctx context.Context, cid, id, ifMatch string, request groupPatchRequest) (*Group, error) {
	unlock := s.lock(id)
	defer unlock()
	storedGroup, err := s.findGroup(ctx, cid, id)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, notFoundResource("SCIM Group")
		}
		return nil, unavailable("could not load SCIM group")
	}
	if ifMatch != "" {
		return nil, notImplementedPatch("conditional Group PATCH is not supported")
	}
	old, initialTuples, err := s.groupValueAndTuples(ctx, storedGroup)
	if err != nil {
		return nil, err
	}
	state := make(map[string]patchMemberState, len(old.Members))
	for _, member := range old.Members {
		resolved, err := s.resolvePatchMember(ctx, cid, id, member)
		if err != nil {
			return nil, err
		}
		state[tupleKey(resolved.tuple)] = resolved
	}
	for _, operation := range request.operations {
		switch operation.op {
		case "add":
			for _, member := range operation.members {
				resolved, err := s.resolvePatchMember(ctx, cid, id, member)
				if err != nil {
					return nil, err
				}
				key := tupleKey(resolved.tuple)
				if _, exists := state[key]; !exists {
					state[key] = resolved
				}
			}
		case "remove":
			resolved, err := s.resolvePatchMemberID(ctx, cid, id, operation.path.memberID)
			if err != nil {
				return nil, err
			}
			delete(state, tupleKey(resolved.tuple))
		}
	}
	keys := make([]string, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := state[keys[i]].member, state[keys[j]].member
		if left.Value != right.Value {
			return left.Value < right.Value
		}
		return left.Type < right.Type
	})
	finalMembers := make([]Member, 0, len(keys))
	finalTuples := make([]*proto.RelationshipTuple, 0, len(keys))
	for _, key := range keys {
		finalMembers = append(finalMembers, state[key].member)
		finalTuples = append(finalTuples, state[key].tuple)
	}
	// Keep the public representation in exactly the same canonical order as
	// groupValue, so a PATCH response and an immediate GET hash identically.
	sortMembers(finalMembers)
	updates := relationshipDiffUpdates(initialTuples, finalTuples)
	if len(updates) == 0 {
		return &old, nil
	}
	if _, err := s.authorization.WriteRelationships(ctx, &proto.WriteRelationshipsRequest{Updates: updates}); err != nil {
		return nil, unavailable("could not apply group membership atomically")
	}
	// Membership is canonical in the provider. Timestamp maintenance is only
	// informational and must not turn a committed provider mutation into a
	// failed SCIM request.
	touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	if err := s.touchAppliedRelationships(touchCtx, updates, nil); err != nil {
		slog.WarnContext(touchCtx, "SCIM Group PATCH committed but metadata touch failed", "group_id", id, "error", err)
	}
	cancel()
	next := old
	next.Members = finalMembers
	next.Meta.LastModified = s.nowUTC()
	if touched, err := s.findGroup(ctx, cid, id); err == nil && !touched.UpdatedAt.IsZero() {
		next.Meta.LastModified = touched.UpdatedAt
	}
	next.Meta.Version = groupContentETag(next)
	return &next, nil
}

func (s *CompactService) resolvePatchMember(ctx context.Context, cid, gid string, member Member) (patchMemberState, error) {
	tuple, err := s.groupTuple(ctx, cid, gid, member)
	if err != nil {
		return patchMemberState{}, err
	}
	resourceType := "Group"
	ref := s.baseURL + "/scim/v2/Groups/" + member.Value
	if tuple.GetTarget().GetSubject() != nil {
		resourceType = "User"
		ref = s.baseURL + "/scim/v2/Users/" + member.Value
	}
	return patchMemberState{member: Member{Value: member.Value, Ref: ref, Type: resourceType}, tuple: tuple}, nil
}

func (s *CompactService) resolvePatchMemberID(ctx context.Context, cid, gid, id string) (patchMemberState, error) {
	if user, err := s.findUser(ctx, cid, id); err == nil {
		return s.resolvePatchMember(ctx, cid, gid, Member{Value: user.ID, Type: "User", Ref: s.baseURL + "/scim/v2/Users/" + user.ID})
	} else if !errors.Is(err, idb.ErrNotFound) {
		return patchMemberState{}, unavailable("could not validate group member")
	}
	if group, err := s.findGroup(ctx, cid, id); err == nil {
		return s.resolvePatchMember(ctx, cid, gid, Member{Value: group.ID, Type: "Group", Ref: s.baseURL + "/scim/v2/Groups/" + group.ID})
	} else if !errors.Is(err, idb.ErrNotFound) {
		return patchMemberState{}, unavailable("could not validate group member")
	}
	return patchMemberState{}, noTarget("group member must reference a User or Group in this SCIM client")
}

func relationshipDiffUpdates(from, to []*proto.RelationshipTuple) []*proto.RelationshipUpdate {
	fromByKey := map[string][]*proto.RelationshipTuple{}
	for _, tuple := range from {
		fromByKey[tupleKey(tuple)] = append(fromByKey[tupleKey(tuple)], tuple)
	}
	toByKey := map[string]*proto.RelationshipTuple{}
	for _, tuple := range to {
		if _, exists := toByKey[tupleKey(tuple)]; !exists {
			toByKey[tupleKey(tuple)] = tuple
		}
	}
	type change struct {
		logical, physical string
		tuple             *proto.RelationshipTuple
	}
	deletes := []change{}
	for key, tuples := range fromByKey {
		if _, keep := toByKey[key]; keep {
			continue
		}
		for _, tuple := range tuples {
			deletes = append(deletes, change{logical: key, physical: physicalTupleKey(tuple), tuple: tuple})
		}
	}
	adds := []change{}
	for key, tuple := range toByKey {
		if _, exists := fromByKey[key]; !exists {
			adds = append(adds, change{logical: key, tuple: tuple})
		}
	}
	sort.Slice(deletes, func(i, j int) bool {
		if deletes[i].logical != deletes[j].logical {
			return deletes[i].logical < deletes[j].logical
		}
		return deletes[i].physical < deletes[j].physical
	})
	sort.Slice(adds, func(i, j int) bool { return adds[i].logical < adds[j].logical })
	updates := make([]*proto.RelationshipUpdate, 0, len(deletes)+len(adds))
	for _, item := range deletes {
		updates = append(updates, &proto.RelationshipUpdate{Operation: proto.RelationshipUpdate_OPERATION_DELETE, Relationship: &proto.Relationship{Tuple: item.tuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}})
	}
	for _, item := range adds {
		updates = append(updates, &proto.RelationshipUpdate{Operation: proto.RelationshipUpdate_OPERATION_TOUCH, Relationship: &proto.Relationship{Tuple: item.tuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}})
	}
	return updates
}

func groupContentETag(group Group) string {
	canon := struct {
		Schemas     []string `json:"schemas"`
		ID          string   `json:"id"`
		ExternalID  string   `json:"externalId,omitempty"`
		DisplayName string   `json:"displayName"`
		Members     []Member `json:"members,omitempty"`
	}{group.Schemas, group.ID, group.ExternalID, group.DisplayName, group.Members}
	return etag(canon)
}
