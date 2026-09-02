package scim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type storedGroup struct {
	id                   string
	clientID             string
	resource             persistedGroup
	pendingResource      persistedGroup
	version              int64
	deleted              bool
	createdAt            time.Time
	updatedAt            time.Time
	lastFingerprint      string
	pending              bool
	pendingDeleted       bool
	pendingVersion       int64
	pendingFingerprint   string
	pendingAffectedUsers []string
	pendingAttemptCount  int64
	pendingNextAttemptAt time.Time
}

type groupMutation struct {
	resource      persistedGroup
	deleted       bool
	fingerprint   string
	affectedUsers []string
}

func (s *Service) CreateGroup(ctx context.Context, clientID string, input groupInput) (*Group, error) {
	resource, err := createGroupResource(input)
	if err != nil {
		return nil, err
	}
	if err := s.validateGroupMembers(ctx, clientID, resource.Members); err != nil {
		return nil, err
	}
	fingerprint := operationFingerprint("create-group", "", resource)
	unlock := s.lock("create-group\x00" + clientID + "\x00" + fingerprint)
	defer unlock()
	if existing, found, findErr := s.findPendingGroup(ctx, clientID, fingerprint); findErr != nil {
		return nil, findErr
	} else if found {
		if err := s.convergeGroup(ctx, existing.id); err != nil {
			return nil, err
		}
		return s.GetGroup(ctx, clientID, existing.id)
	}
	id := uuid.NewString()
	mutation := groupMutation{resource: resource, fingerprint: fingerprint}
	mutation.affectedUsers = s.affectedUsers(ctx, clientID, nil, resource.Members)
	if err := s.persistGroupMutation(ctx, id, clientID, storedGroup{resource: persistedGroup{DisplayName: resource.DisplayName}}, mutation, true); err != nil {
		return nil, err
	}
	if err := s.convergeGroup(ctx, id); err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, clientID, id)
}

func createGroupResource(input groupInput) (persistedGroup, error) {
	resource := persistedGroup{}
	if input.ExternalID != nil {
		resource.ExternalID = *input.ExternalID
	}
	if input.DisplayName != nil {
		resource.DisplayName = *input.DisplayName
	}
	if strings.TrimSpace(resource.DisplayName) == "" {
		return persistedGroup{}, invalid("displayName is required")
	}
	if input.Members != nil {
		resource.Members = append([]Member(nil), (*input.Members)...)
	}
	if resource.Members == nil {
		resource.Members = []Member{}
	}
	return resource, nil
}

func (s *Service) GetGroup(ctx context.Context, clientID, id string) (*Group, error) {
	row, err := s.loadGroup(ctx, clientID, id, false)
	if err != nil {
		return nil, err
	}
	if row.pending && row.version == 0 {
		return nil, unavailable("SCIM Group projection has not converged")
	}
	group, err := s.publicGroup(ctx, row)
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (s *Service) listGroups(ctx context.Context, clientID, rawFilter string, startIndex, count int) (listResponse[Group], error) {
	clauses, err := parseGroupFilter(rawFilter)
	if err != nil {
		return listResponse[Group]{}, invalidFilter(err.Error())
	}
	records, err := s.db.ObjectStore(coredata.StoreSCIMGroups).Index("by_client").GetAll(ctx, clientID)
	if err != nil {
		return listResponse[Group]{}, unavailable("could not list SCIM Groups")
	}
	rows := make([]storedGroup, 0, len(records))
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			return listResponse[Group]{}, unavailable("stored SCIM Group is invalid")
		}
		if row.deleted || row.pending && row.version == 0 || !matchesGroupFilter(row.resource, clauses) {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	total := len(rows)
	begin := startIndex - 1
	if begin > total {
		begin = total
	}
	end := begin + count
	if end > total {
		end = total
	}
	resources := make([]Group, 0, end-begin)
	for i := begin; i < end; i++ {
		group, publicErr := s.publicGroup(ctx, rows[i])
		if publicErr != nil {
			return listResponse[Group]{}, publicErr
		}
		resources = append(resources, group)
	}
	return listResponse[Group]{Schemas: []string{ListSchemaURN}, TotalResults: total, StartIndex: startIndex, ItemsPerPage: len(resources), Resources: resources}, nil
}

func (s *Service) ReplaceGroup(ctx context.Context, clientID, id, ifMatch string, input groupInput) (*Group, error) {
	resource, err := createGroupResource(input)
	if err != nil {
		return nil, err
	}
	fingerprint := operationFingerprint("replace-group", ifMatch, resource)
	return s.mutateGroup(ctx, clientID, id, ifMatch, fingerprint, func(persistedGroup) (persistedGroup, error) { return resource, nil })
}

func (s *Service) PatchGroup(ctx context.Context, clientID, id, ifMatch string, request patchRequest) (*Group, error) {
	fingerprint := patchOperationFingerprint(ifMatch, request)
	return s.mutateGroup(ctx, clientID, id, ifMatch, fingerprint, func(current persistedGroup) (persistedGroup, error) {
		return applyGroupPatch(current, request)
	})
}

func (s *Service) mutateGroup(ctx context.Context, clientID, id, ifMatch, fingerprint string, propose func(persistedGroup) (persistedGroup, error)) (*Group, error) {
	unlock := s.lock(userLockKey("group:" + id))
	defer unlock()
	if err := s.convergeGroupLocked(ctx, id); err != nil {
		return nil, err
	}
	current, err := s.loadGroup(ctx, clientID, id, false)
	if err != nil {
		return nil, err
	}
	if current.lastFingerprint == fingerprint {
		group, publicErr := s.publicGroup(ctx, current)
		return &group, publicErr
	}
	if ifMatch != "" && ifMatch != "*" && ifMatch != etag(current.version) {
		return nil, &Error{Status: http.StatusPreconditionFailed, Detail: "If-Match does not match the current SCIM Group version"}
	}
	resource, err := propose(current.resource)
	if err != nil {
		return nil, err
	}
	if err := s.validateGroupMembers(ctx, clientID, resource.Members); err != nil {
		return nil, err
	}
	if err := validateImmutableMembers(current.resource.Members, resource.Members); err != nil {
		return nil, err
	}
	mutation := groupMutation{resource: resource, fingerprint: fingerprint}
	mutation.affectedUsers = s.affectedUsers(ctx, clientID, current.resource.Members, resource.Members)
	if err := s.persistGroupMutation(ctx, id, clientID, current, mutation, false); err != nil {
		return nil, err
	}
	if err := s.convergeGroupLocked(ctx, id); err != nil {
		return nil, err
	}
	return s.GetGroup(ctx, clientID, id)
}

func (s *Service) DeleteGroup(ctx context.Context, clientID, id, ifMatch string) error {
	if err := s.deleteGroup(ctx, clientID, id, ifMatch); err != nil {
		return err
	}
	// Cascade after releasing the deleted Group's lock. Nested Groups may be
	// cyclic, and concurrent deletes must not acquire locks in opposite order.
	return s.removeGroupFromParents(ctx, clientID, id)
}

func (s *Service) deleteGroup(ctx context.Context, clientID, id, ifMatch string) error {
	fingerprint := operationFingerprint("delete-group", ifMatch, nil)
	unlock := s.lock(userLockKey("group:" + id))
	defer unlock()
	current, err := s.loadGroup(ctx, clientID, id, true)
	if err != nil {
		return err
	}
	if current.deleted {
		return notFoundResource("SCIM Group")
	}
	if current.pending {
		pendingFingerprint := current.pendingFingerprint
		if err := s.convergeGroupLocked(ctx, id); err != nil {
			return err
		}
		current, err = s.loadGroup(ctx, clientID, id, true)
		if err != nil {
			return err
		}
		if current.deleted {
			if pendingFingerprint == fingerprint {
				return nil
			}
			return notFoundResource("SCIM Group")
		}
	}
	if ifMatch != "" && ifMatch != "*" && ifMatch != etag(current.version) {
		return &Error{Status: http.StatusPreconditionFailed, Detail: "If-Match does not match the current SCIM Group version"}
	}
	mutation := groupMutation{resource: current.resource, fingerprint: fingerprint, deleted: true}
	mutation.affectedUsers = s.affectedUsers(ctx, clientID, current.resource.Members, nil)
	if err := s.persistGroupMutation(ctx, id, clientID, current, mutation, false); err != nil {
		return err
	}
	if err := s.convergeGroupLocked(ctx, id); err != nil {
		return err
	}
	return nil
}

func (s *Service) persistGroupMutation(ctx context.Context, id, clientID string, current storedGroup, mutation groupMutation, creating bool) error {
	now := s.currentTime()
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMGroups}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return unavailable("could not begin SCIM Group mutation")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	store := tx.ObjectStore(coredata.StoreSCIMGroups)
	if creating {
		if err := store.Add(ctx, groupPendingRecord(id, clientID, mutation, now)); err != nil {
			return unavailable("could not persist SCIM Group creation")
		}
	} else {
		rec, err := store.Get(ctx, id)
		if errors.Is(err, idb.ErrNotFound) {
			return notFoundResource("SCIM Group")
		}
		if err != nil {
			return unavailable("could not verify current SCIM Group")
		}
		latest, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil || latest.clientID != clientID || latest.deleted || latest.version != current.version || latest.pending {
			return &Error{Status: http.StatusPreconditionFailed, Detail: "SCIM Group changed during mutation"}
		}
		rec["pending_resource"] = jsonMap(mutation.resource)
		rec["pending_deleted"] = mutation.deleted
		rec["pending_version"] = current.version + 1
		rec["pending_fingerprint"] = mutation.fingerprint
		rec["pending_affected_users"] = jsonSlice(mutation.affectedUsers)
		rec["pending_attempt_count"] = int64(0)
		rec["pending_next_attempt_at"] = now
		if err := store.Put(ctx, rec); err != nil {
			return unavailable("could not persist SCIM Group mutation")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("could not commit SCIM Group mutation")
	}
	committed = true
	return nil
}

func groupPendingRecord(id, clientID string, mutation groupMutation, now time.Time) idb.Record {
	record := idb.Record{
		"id": id, "client_id": clientID,
		"version": int64(0), "deleted": false, "resource": jsonMap(persistedGroup{}),
		"pending_resource": jsonMap(mutation.resource), "pending_deleted": mutation.deleted, "pending_version": int64(1),
		"pending_fingerprint": mutation.fingerprint, "pending_affected_users": jsonSlice(mutation.affectedUsers), "pending_attempt_count": int64(0),
		"pending_next_attempt_at": now, "created_at": now, "updated_at": now,
	}
	return record
}

func (s *Service) convergeGroup(ctx context.Context, id string) error {
	unlock := s.lock(userLockKey("group:" + id))
	defer unlock()
	return s.convergeGroupLocked(ctx, id)
}

func (s *Service) convergeGroupLocked(ctx context.Context, id string) error {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMGroups).Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) {
		return notFoundResource("SCIM Group")
	}
	if err != nil {
		return unavailable("could not load pending SCIM Group mutation")
	}
	row, err := decodeStoredGroup(rec)
	if err != nil {
		return unavailable("pending SCIM Group is invalid")
	}
	if !row.pending {
		return nil
	}
	desiredMembers := row.pendingResource.Members
	if row.pendingDeleted {
		desiredMembers = nil
	}
	if err := s.applyGroupProjections(ctx, row.clientID, row.id, row.resource.Members, desiredMembers); err != nil {
		_ = s.recordGroupProjectionFailure(ctx, id)
		return unavailable("authorization Group projection has not converged")
	}
	now := s.currentTime()
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMGroups}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return unavailable("could not commit SCIM Group mutation")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	latestRec, err := tx.ObjectStore(coredata.StoreSCIMGroups).Get(ctx, id)
	if err != nil {
		return unavailable("could not verify pending SCIM Group mutation")
	}
	latest, err := decodeStoredGroup(latestRec)
	if err != nil || !latest.pending || latest.pendingFingerprint != row.pendingFingerprint || latest.pendingVersion != row.pendingVersion {
		return unavailable("SCIM Group mutation was superseded")
	}
	desired := latest.pendingResource
	latestRec["resource"] = jsonMap(desired)
	latestRec["version"] = latest.pendingVersion
	latestRec["deleted"] = latest.pendingDeleted
	latestRec["updated_at"] = now
	latestRec["last_operation_fingerprint"] = latest.pendingFingerprint
	for _, key := range []string{"pending_resource", "pending_deleted", "pending_version", "pending_fingerprint", "pending_affected_users", "pending_attempt_count", "pending_next_attempt_at"} {
		delete(latestRec, key)
	}
	if err := tx.ObjectStore(coredata.StoreSCIMGroups).Put(ctx, latestRec); err != nil {
		return unavailable("could not commit SCIM Group")
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("could not commit SCIM Group")
	}
	committed = true
	return nil
}

func (s *Service) applyGroupProjections(ctx context.Context, clientID, groupID string, from, to []Member) error {
	fromTuples, err := s.groupMemberTuples(ctx, clientID, groupID, from)
	if err != nil {
		return err
	}
	toTuples, err := s.groupMemberTuples(ctx, clientID, groupID, to)
	if err != nil {
		return err
	}
	fromSet, toSet := make(map[string]*proto.RelationshipTuple), make(map[string]*proto.RelationshipTuple)
	for _, tuple := range fromTuples {
		fromSet[relationshipTupleKey(tuple)] = tuple
	}
	for _, tuple := range toTuples {
		toSet[relationshipTupleKey(tuple)] = tuple
	}
	if s.authorization == nil {
		// A deployment without an authorization provider can still act as a
		// standards-compliant SCIM resource store. There is no projection to
		// converge in that mode; configuration validation rejects lifecycle
		// gates and static projections that would require one.
		return nil
	}
	for key, tuple := range fromSet {
		if _, keep := toSet[key]; keep {
			continue
		}
		existing, err := s.findRelationship(ctx, tuple)
		if err != nil {
			return err
		}
		if existing == nil || !groupRelationshipOwnedBy(existing, clientID, groupID) {
			continue
		}
		if _, err := s.authorization.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{RelationshipTuple: tuple}); err != nil && !errors.Is(err, core.ErrNotFound) {
			return err
		}
	}
	for key, tuple := range toSet {
		if _, existed := fromSet[key]; existed {
			continue
		}
		existing, err := s.findRelationship(ctx, tuple)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		properties, _ := structpb.NewStruct(map[string]any{"managedBy": "scim", "scimClientId": clientID, "scimGroupId": groupID})
		if _, err := s.authorization.AddRelationship(ctx, &proto.AddRelationshipRequest{Relationship: &proto.Relationship{Tuple: tuple, Properties: properties, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) groupMemberTuples(ctx context.Context, clientID, groupID string, members []Member) ([]*proto.RelationshipTuple, error) {
	out := make([]*proto.RelationshipTuple, 0, len(members))
	seen := make(map[string]struct{})
	for _, member := range members {
		tuple, err := s.groupMemberTuple(ctx, clientID, groupID, member)
		if err != nil {
			return nil, err
		}
		key := relationshipTupleKey(tuple)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tuple)
	}
	return out, nil
}

func (s *Service) groupMemberTuple(ctx context.Context, clientID, groupID string, member Member) (*proto.RelationshipTuple, error) {
	memberType, err := s.memberType(ctx, clientID, member, true)
	if err != nil {
		return nil, err
	}
	target := &proto.RelationshipTarget{}
	if memberType == "User" {
		row, err := s.loadUserRow(ctx, clientID, member.Value, true)
		if err != nil {
			return nil, err
		}
		target.Kind = &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + row.coreUserID}}
	} else {
		target.Kind = &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: "group", Id: member.Value}, Relation: "member"}}
	}
	return &proto.RelationshipTuple{Target: target, Relation: "member", Resource: &proto.Resource{Type: "group", Id: groupID}}, nil
}

func (s *Service) memberType(ctx context.Context, clientID string, member Member, includeDeleted bool) (string, error) {
	requested := strings.ToLower(strings.TrimSpace(member.Type))
	if requested != "" && requested != "user" && requested != "group" {
		return "", invalid("members.type must be User or Group")
	}
	if _, err := s.loadUserRow(ctx, clientID, member.Value, includeDeleted); err == nil {
		if requested != "" && requested != "user" {
			return "", invalid("member type does not match the referenced resource")
		}
		return "User", nil
	} else if isServiceUnavailable(err) {
		return "", err
	}
	if _, err := s.loadGroup(ctx, clientID, member.Value, includeDeleted); err == nil {
		if requested != "" && requested != "group" {
			return "", invalid("member type does not match the referenced resource")
		}
		return "Group", nil
	} else if isServiceUnavailable(err) {
		return "", err
	}
	return "", invalid("member value must reference a User or Group in this SCIM client")
}

func relationshipTupleKey(tuple *proto.RelationshipTuple) string {
	return tupleKeyTarget(tuple.GetTarget()) + "\x00" + tuple.GetRelation() + "\x00" + tuple.GetResource().GetType() + "\x00" + tuple.GetResource().GetId()
}

func tupleKeyTarget(target *proto.RelationshipTarget) string {
	if subject := target.GetSubject(); subject != nil {
		return "subject\x00" + subject.GetType() + "\x00" + subject.GetId()
	}
	set := target.GetSubjectSet()
	if set == nil {
		return "empty"
	}
	return "set\x00" + set.GetResource().GetType() + "\x00" + set.GetResource().GetId() + "\x00" + set.GetRelation()
}

func (s *Service) findRelationship(ctx context.Context, tuple *proto.RelationshipTuple) (*proto.Relationship, error) {
	if s.authorization == nil {
		return nil, errors.New("authorization provider is unavailable")
	}
	response, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{Filter: &proto.RelationshipFilter{Target: tuple.Target, Relation: tuple.Relation, Resource: tuple.Resource}, PageSize: 2})
	if err != nil {
		return nil, err
	}
	for _, relationship := range response.GetRelationships() {
		if gproto.Equal(relationship.GetTuple(), tuple) {
			return relationship, nil
		}
	}
	return nil, nil
}

func groupRelationshipOwnedBy(relationship *proto.Relationship, clientID, groupID string) bool {
	return relationshipOwnedBySCIM(relationship, clientID, "scimGroupId", groupID)
}

func relationshipOwnedBySCIM(relationship *proto.Relationship, clientID, idProperty, id string) bool {
	if relationship == nil || relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || relationship.GetProperties() == nil {
		return false
	}
	props := relationship.GetProperties().AsMap()
	return props["managedBy"] == "scim" && props["scimClientId"] == clientID && props[idProperty] == id
}

func (s *Service) loadGroup(ctx context.Context, clientID, id string, includeDeleted bool) (storedGroup, error) {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMGroups).Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) {
		return storedGroup{}, notFoundResource("SCIM Group")
	}
	if err != nil {
		return storedGroup{}, unavailable("could not read SCIM Group")
	}
	row, err := decodeStoredGroup(rec)
	if err != nil {
		return storedGroup{}, unavailable("stored SCIM Group is invalid")
	}
	if row.clientID != clientID || row.deleted && !includeDeleted {
		return storedGroup{}, notFoundResource("SCIM Group")
	}
	return row, nil
}

func (s *Service) publicGroup(ctx context.Context, row storedGroup) (Group, error) {
	members := append([]Member(nil), row.resource.Members...)
	for i := range members {
		members[i].Display = ""
		memberType, err := s.memberType(ctx, row.clientID, members[i], false)
		if err != nil {
			// Deleted references are omitted by cascade, but a historical
			// resource may contain one during repair. Preserve the value and
			// leave its vendor-supplied formatting intact.
			continue
		}
		if strings.TrimSpace(members[i].Type) == "" {
			members[i].Type = memberType
		}
		if strings.TrimSpace(members[i].Ref) == "" {
			if memberType == "User" {
				members[i].Ref = s.baseURL + "/scim/v2/Users/" + members[i].Value
			} else {
				members[i].Ref = s.baseURL + "/scim/v2/Groups/" + members[i].Value
			}
		}
		members[i].Display = s.memberDisplayName(ctx, row.clientID, members[i], memberType)
	}
	return Group{Schemas: []string{GroupSchemaURN}, ID: row.id, ExternalID: row.resource.ExternalID, DisplayName: row.resource.DisplayName, Members: members, Meta: Meta{ResourceType: "Group", Created: row.createdAt, LastModified: row.updatedAt, Location: s.baseURL + "/scim/v2/Groups/" + row.id, Version: etag(row.version)}}, nil
}

func (s *Service) memberDisplayName(ctx context.Context, clientID string, member Member, memberType string) string {
	if memberType == "User" {
		row, err := s.loadUserRow(ctx, clientID, member.Value, true)
		if err == nil {
			return displayName(row.resource)
		}
	} else if row, err := s.loadGroup(ctx, clientID, member.Value, true); err == nil {
		return row.resource.DisplayName
	}
	return ""
}

func isServiceUnavailable(err error) bool {
	var scimErr *Error
	return errors.As(err, &scimErr) && scimErr.Status == 503
}

func (s *Service) validateGroupMembers(ctx context.Context, clientID string, members []Member) error {
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.Value) == "" {
			return invalid("members.value is required")
		}
		if _, duplicate := seen[member.Value]; duplicate {
			return invalid("members.value must be unique")
		}
		seen[member.Value] = struct{}{}
		if _, err := s.memberType(ctx, clientID, member, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) findPendingGroup(ctx context.Context, clientID, fingerprint string) (storedGroup, bool, error) {
	records, err := s.db.ObjectStore(coredata.StoreSCIMGroups).Index("by_client").GetAll(ctx, clientID)
	if err != nil {
		return storedGroup{}, false, unavailable("could not inspect pending SCIM Group creation")
	}
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			return storedGroup{}, false, unavailable("stored SCIM Group is invalid")
		}
		if row.pending && row.pendingFingerprint == fingerprint {
			return row, true, nil
		}
	}
	return storedGroup{}, false, nil
}

func applyGroupPatch(current persistedGroup, request patchRequest) (persistedGroup, error) {
	if len(request.Operations) == 0 {
		return persistedGroup{}, invalid("PATCH Operations must not be empty")
	}
	result := current
	for i, operation := range request.Operations {
		op := strings.ToLower(strings.TrimSpace(operation.Op))
		if op != "add" && op != "replace" && op != "remove" {
			return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d].op is unsupported", i))
		}
		path := normalizePatchPath(operation.Path)
		if path == "" {
			if op == "remove" {
				return persistedGroup{}, invalid("remove requires a path")
			}
			var input groupInput
			if err := json.Unmarshal(operation.Value, &input); err != nil {
				return persistedGroup{}, invalid("pathless PATCH value must be an object")
			}
			if input.DisplayName != nil {
				result.DisplayName = *input.DisplayName
			}
			if input.ExternalID != nil {
				result.ExternalID = *input.ExternalID
			}
			if input.Members != nil {
				if op == "add" {
					result.Members = appendUniqueMembers(result.Members, *input.Members)
				} else {
					result.Members = append([]Member(nil), (*input.Members)...)
				}
			}
			continue
		}
		if strings.HasPrefix(path, "members[") {
			value, ok := parseGroupMemberFilter(path)
			if !ok {
				return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d] has an invalid members path", i))
			}
			switch op {
			case "remove":
				filtered := result.Members[:0]
				found := false
				for _, member := range result.Members {
					if member.Value != value {
						filtered = append(filtered, member)
					} else {
						found = true
					}
				}
				if !found {
					return persistedGroup{}, noTarget(fmt.Sprintf("Operations[%d] member was not found", i))
				}
				result.Members = filtered
			case "replace":
				var replacement Member
				if err := json.Unmarshal(operation.Value, &replacement); err != nil || strings.TrimSpace(replacement.Value) == "" {
					return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d] member replacement is invalid", i))
				}
				if replacement.Value != value {
					return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d] members.value is immutable", i))
				}
				found := false
				for j := range result.Members {
					if result.Members[j].Value == value {
						result.Members[j] = replacement
						found = true
					}
				}
				if !found {
					return persistedGroup{}, noTarget(fmt.Sprintf("Operations[%d] member was not found", i))
				}
			case "add":
				return persistedGroup{}, invalid("add is not supported for a filtered members path")
			}
			continue
		}
		switch path {
		case "displayname":
			if op == "remove" {
				return persistedGroup{}, invalid("displayName cannot be removed")
			}
			if err := decodePatchValue(operation.Value, &result.DisplayName); err != nil {
				return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d]: %v", i, err))
			}
		case "externalid":
			if op == "remove" {
				result.ExternalID = ""
			} else if err := decodePatchValue(operation.Value, &result.ExternalID); err != nil {
				return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d]: %v", i, err))
			}
		case "members":
			if op == "remove" {
				result.Members = []Member{}
				continue
			}
			members, err := decodeMembers(operation.Value)
			if err != nil {
				return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d]: %v", i, err))
			}
			if op == "add" {
				result.Members = appendUniqueMembers(result.Members, members)
			} else {
				result.Members = members
			}
		default:
			return persistedGroup{}, invalid(fmt.Sprintf("Operations[%d] has unsupported path %q", i, path))
		}
	}
	if strings.TrimSpace(result.DisplayName) == "" {
		return persistedGroup{}, invalid("displayName is required")
	}
	return result, nil
}

func appendUniqueMembers(existing, additions []Member) []Member {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	result := append([]Member(nil), existing...)
	for _, member := range result {
		seen[member.Value] = struct{}{}
	}
	for _, member := range additions {
		if _, ok := seen[member.Value]; ok {
			continue
		}
		seen[member.Value] = struct{}{}
		result = append(result, member)
	}
	return result
}

func validateImmutableMembers(previous, next []Member) error {
	byValue := make(map[string]Member, len(previous))
	for _, member := range previous {
		byValue[member.Value] = member
	}
	for _, member := range next {
		old, ok := byValue[member.Value]
		if !ok {
			continue
		}
		if old.Ref != "" && member.Ref != "" && old.Ref != member.Ref {
			return invalid("members.$ref is immutable")
		}
		if old.Type != "" && member.Type != "" && !strings.EqualFold(old.Type, member.Type) {
			return invalid("members.type is immutable")
		}
	}
	return nil
}

func decodeMembers(raw json.RawMessage) ([]Member, error) {
	var many []Member
	if err := json.Unmarshal(raw, &many); err == nil {
		return many, nil
	}
	var one Member
	if err := json.Unmarshal(raw, &one); err != nil || strings.TrimSpace(one.Value) == "" {
		return nil, errors.New("members must be an object or array")
	}
	return []Member{one}, nil
}

func decodeStoredGroup(rec idb.Record) (storedGroup, error) {
	var resource, pendingResource persistedGroup
	var affected []string
	if err := decodeJSONValue(rec["resource"], &resource); err != nil {
		return storedGroup{}, err
	}
	if err := decodeJSONValue(rec["pending_resource"], &pendingResource); err != nil {
		return storedGroup{}, err
	}
	if err := decodeJSONValue(rec["pending_affected_users"], &affected); err != nil {
		return storedGroup{}, err
	}
	pendingVersion := recordInt(rec, "pending_version")
	return storedGroup{id: recordString(rec, "id"), clientID: recordString(rec, "client_id"), resource: resource, pendingResource: pendingResource, version: recordInt(rec, "version"), deleted: recordBool(rec, "deleted"), createdAt: recordTime(rec, "created_at"), updatedAt: recordTime(rec, "updated_at"), lastFingerprint: recordString(rec, "last_operation_fingerprint"), pending: pendingVersion > 0, pendingDeleted: recordBool(rec, "pending_deleted"), pendingVersion: pendingVersion, pendingFingerprint: recordString(rec, "pending_fingerprint"), pendingAffectedUsers: affected, pendingAttemptCount: recordInt(rec, "pending_attempt_count"), pendingNextAttemptAt: recordTime(rec, "pending_next_attempt_at")}, nil
}

func (s *Service) recordGroupProjectionFailure(ctx context.Context, id string) error {
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMGroups}, idb.TransactionReadwrite, idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()
	rec, err := tx.ObjectStore(coredata.StoreSCIMGroups).Get(ctx, id)
	if err != nil {
		return err
	}
	row, err := decodeStoredGroup(rec)
	if err != nil || !row.pending {
		return err
	}
	attempt := row.pendingAttemptCount + 1
	delay := time.Second << min(attempt-1, 6)
	if delay > time.Minute {
		delay = time.Minute
	}
	now := s.currentTime()
	rec["pending_attempt_count"] = attempt
	rec["pending_next_attempt_at"] = now.Add(delay)
	if err := tx.ObjectStore(coredata.StoreSCIMGroups).Put(ctx, rec); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Service) reconcilePendingGroups(ctx context.Context) (int, error) {
	records, err := s.db.ObjectStore(coredata.StoreSCIMGroups).GetAll(ctx, nil)
	if err != nil {
		return 0, err
	}
	processed := 0
	var errs []error
	now := s.currentTime()
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			errs = append(errs, decodeErr)
			continue
		}
		if !row.pending || row.pendingNextAttemptAt.After(now) {
			continue
		}
		processed++
		if err := s.convergeGroup(ctx, row.id); err != nil {
			errs = append(errs, err)
		}
	}
	return processed, errors.Join(errs...)
}

func (s *Service) reconcileGroupDrift(ctx context.Context) (int, error) {
	var errs []error
	if err := s.reconcileDeletedUserReferences(ctx); err != nil {
		errs = append(errs, err)
	}
	records, err := s.db.ObjectStore(coredata.StoreSCIMGroups).GetAll(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			errs = append(errs, decodeErr)
			continue
		}
		if row.deleted {
			if err := s.removeGroupFromParents(ctx, row.clientID, row.id); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Reference cleanup can commit newer parent resources, so reload before
	// repairing authorization projection drift.
	records, err = s.db.ObjectStore(coredata.StoreSCIMGroups).GetAll(ctx, nil)
	if err != nil {
		errs = append(errs, err)
		return 0, errors.Join(errs...)
	}
	processed := 0
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			errs = append(errs, decodeErr)
			continue
		}
		if row.deleted || row.pending {
			continue
		}
		processed++
		if err := s.reconcileGroupProjectionDrift(ctx, row); err != nil {
			errs = append(errs, err)
		}
	}
	return processed, errors.Join(errs...)
}

func (s *Service) reconcileDeletedUserReferences(ctx context.Context) error {
	records, err := s.db.ObjectStore(coredata.StoreSCIMUsers).GetAll(ctx, nil)
	if err != nil {
		return err
	}
	var errs []error
	for _, rec := range records {
		row, decodeErr := decodeStoredRow(rec)
		if decodeErr != nil {
			errs = append(errs, decodeErr)
			continue
		}
		if !row.deleted {
			continue
		}
		if err := s.removeUserFromGroups(ctx, row.clientID, row.id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) reconcileGroupProjectionDrift(ctx context.Context, row storedGroup) error {
	desiredTuples, err := s.groupMemberTuples(ctx, row.clientID, row.id, row.resource.Members)
	if err != nil {
		return err
	}
	desired := make(map[string]struct{}, len(desiredTuples))
	for _, tuple := range desiredTuples {
		desired[relationshipTupleKey(tuple)] = struct{}{}
	}
	if s.authorization == nil {
		return nil
	}
	pageToken := ""
	for {
		response, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{Filter: &proto.RelationshipFilter{Relation: "member", Resource: &proto.Resource{Type: "group", Id: row.id}}, PageSize: 200, PageToken: pageToken})
		if err != nil {
			return err
		}
		for _, relationship := range response.GetRelationships() {
			tuple := relationship.GetTuple()
			if _, keep := desired[relationshipTupleKey(tuple)]; keep || !groupRelationshipOwnedBy(relationship, row.clientID, row.id) {
				continue
			}
			if _, err := s.authorization.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{RelationshipTuple: tuple}); err != nil && !errors.Is(err, core.ErrNotFound) {
				return err
			}
		}
		pageToken = strings.TrimSpace(response.GetNextPageToken())
		if pageToken == "" {
			break
		}
	}
	return s.applyGroupProjections(ctx, row.clientID, row.id, nil, row.resource.Members)
}

func (s *Service) removeGroupFromParents(ctx context.Context, clientID, groupID string) error {
	return s.removeMemberReferences(ctx, clientID, groupID, "Group")
}

func (s *Service) removeUserFromGroups(ctx context.Context, clientID, userID string) error {
	return s.removeMemberReferences(ctx, clientID, userID, "User")
}

func (s *Service) removeMemberReferences(ctx context.Context, clientID, memberID, expectedType string) error {
	records, err := s.db.ObjectStore(coredata.StoreSCIMGroups).Index("by_client").GetAll(ctx, clientID)
	if err != nil {
		return unavailable("could not find SCIM Group references")
	}
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			return unavailable("stored SCIM Group is invalid")
		}
		if row.deleted {
			continue
		}
		if row.pending {
			if err := s.convergeGroup(ctx, row.id); err != nil {
				return err
			}
			row, err = s.loadGroup(ctx, clientID, row.id, false)
			if err != nil {
				var scimErr *Error
				if errors.As(err, &scimErr) && scimErr.Status == http.StatusNotFound {
					continue
				}
				return err
			}
		}
		members := make([]Member, 0, len(row.resource.Members))
		changed := false
		for _, member := range row.resource.Members {
			memberType, typeErr := s.memberType(ctx, clientID, member, false)
			if isServiceUnavailable(typeErr) {
				return typeErr
			}
			if typeErr != nil {
				// This was a valid reference when committed and is now a
				// tombstone. Clean all such references in the same mutation.
				changed = true
				continue
			}
			if member.Value == memberID && memberType == expectedType {
				changed = true
				continue
			}
			members = append(members, member)
		}
		if changed {
			if _, err := s.ReplaceGroup(ctx, clientID, row.id, "*", groupInput{DisplayName: &row.resource.DisplayName, ExternalID: &row.resource.ExternalID, Members: &members}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) affectedUsers(ctx context.Context, clientID string, from, to []Member) []string {
	ids := make(map[string]struct{})
	for _, member := range append(append([]Member{}, from...), to...) {
		if typ, err := s.memberType(ctx, clientID, member, true); err == nil && typ == "User" {
			ids[member.Value] = struct{}{}
		} else if typ == "Group" {
			for userID := range s.collectUsersFromMembers(ctx, clientID, []Member{member}, make(map[string]struct{})) {
				ids[userID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (s *Service) collectUsersFromMembers(ctx context.Context, clientID string, members []Member, seen map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for _, member := range members {
		typ, err := s.memberType(ctx, clientID, member, true)
		if err != nil {
			continue
		}
		if typ == "User" {
			out[member.Value] = struct{}{}
			continue
		}
		if _, ok := seen[member.Value]; ok {
			continue
		}
		seen[member.Value] = struct{}{}
		row, err := s.loadGroup(ctx, clientID, member.Value, true)
		if err != nil {
			continue
		}
		for id := range s.collectUsersFromMembers(ctx, clientID, row.resource.Members, seen) {
			out[id] = struct{}{}
		}
	}
	return out
}

func (s *Service) loadGroupGraph(ctx context.Context, clientID string) (map[string]*persistedGroup, error) {
	records, err := s.db.ObjectStore(coredata.StoreSCIMGroups).Index("by_client").GetAll(ctx, clientID)
	if err != nil {
		return nil, unavailable("could not load User groups")
	}
	groups := make(map[string]*persistedGroup)
	for _, rec := range records {
		row, decodeErr := decodeStoredGroup(rec)
		if decodeErr != nil {
			return nil, unavailable("stored SCIM Group is invalid")
		}
		if !row.deleted && (!row.pending || row.version > 0) {
			group := row.resource
			groups[row.id] = &group
		}
	}
	return groups, nil
}

func (s *Service) groupRefsForUser(groups map[string]*persistedGroup, userID string) []GroupRef {
	ids := make([]string, 0)
	types := make(map[string]string)
	for id := range groups {
		if direct, ok := groupMembershipType(id, userID, groups, make(map[string]struct{})); ok {
			ids = append(ids, id)
			if direct {
				types[id] = "direct"
			} else {
				types[id] = "indirect"
			}
		}
	}
	sort.Strings(ids)
	refs := make([]GroupRef, 0, len(ids))
	for _, id := range ids {
		row := groups[id]
		refs = append(refs, GroupRef{Value: id, Ref: s.baseURL + "/scim/v2/Groups/" + id, Display: row.DisplayName, Type: types[id]})
	}
	return refs
}

// groupMembershipType returns whether the membership is direct (the user is a
// member of this group) or indirect (the user is reached through a nested
// group). The visited set makes malformed cyclic graphs terminate safely.
func groupMembershipType(id, userID string, groups map[string]*persistedGroup, seen map[string]struct{}) (bool, bool) {
	if _, ok := seen[id]; ok {
		return false, false
	}
	seen[id] = struct{}{}
	row, ok := groups[id]
	if !ok {
		return false, false
	}
	indirect := false
	for _, member := range row.Members {
		if strings.EqualFold(strings.TrimSpace(member.Type), "group") || strings.TrimSpace(member.Type) == "" && groups[member.Value] != nil {
			if _, ok := groupMembershipType(member.Value, userID, groups, seen); ok {
				indirect = true
			}
		} else if member.Value == userID {
			return true, true
		}
	}
	return false, indirect
}

func (s *Service) decorateUsers(ctx context.Context, clientID string, users []User) error {
	groups, err := s.loadGroupGraph(ctx, clientID)
	if err != nil {
		return err
	}
	for i := range users {
		users[i].Groups = s.groupRefsForUser(groups, users[i].ID)
	}
	return nil
}

func (s *Service) loadUserRow(ctx context.Context, clientID, id string, includeDeleted bool) (storedRow, error) {
	rec, err := s.db.ObjectStore(coredata.StoreSCIMUsers).Get(ctx, id)
	if errors.Is(err, idb.ErrNotFound) {
		return storedRow{}, invalid("member value must reference a User or Group in this SCIM client")
	}
	if err != nil {
		return storedRow{}, unavailable("could not read SCIM User member")
	}
	row, err := decodeStoredRow(rec)
	if err != nil {
		return storedRow{}, unavailable("stored SCIM User member is invalid")
	}
	if row.clientID != clientID || row.deleted && !includeDeleted {
		return storedRow{}, invalid("member value must reference a User or Group in this SCIM client")
	}
	return row, nil
}
