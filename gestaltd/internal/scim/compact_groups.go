package scim

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func (s *CompactService) groupValue(ctx context.Context, r storedResource) (Group, error) {
	g, _, err := s.groupValueAndTuples(ctx, r)
	return g, err
}

func (s *CompactService) groupValueAndTuples(ctx context.Context, r storedResource) (Group, []*proto.RelationshipTuple, error) {
	g := Group{Schemas: []string{GroupSchemaURN}, ID: r.ID, ExternalID: r.ExternalID, DisplayName: r.DisplayName, Meta: Meta{ResourceType: "Group", Created: r.CreatedAt, LastModified: r.UpdatedAt, Location: s.baseURL + "/scim/v2/Groups/" + r.ID}}
	m, tuples, err := s.liveMembersAndTuples(ctx, r.ClientID, r.ID)
	if err != nil {
		return Group{}, nil, unavailable("could not read group membership")
	}
	g.Members = m
	g.Meta.Version = groupContentETag(g)
	return g, tuples, nil
}

func validateRetainedMemberAttributes(previous, next []Member) error {
	old := make(map[string]Member, len(previous))
	for _, member := range previous {
		old[member.Value] = member
	}
	for _, member := range next {
		before, ok := old[member.Value]
		if !ok {
			continue
		}
		if (member.Ref != "" && member.Ref != before.Ref) || (member.Type != "" && !strings.EqualFold(member.Type, before.Type)) {
			return mutability("group member reference attributes are immutable")
		}
		// display is a read-only derived value. Providers may send it back on
		// replacement; it is ignored rather than treated as client state.
	}
	return nil
}

func (s *CompactService) liveMembersAndTuples(ctx context.Context, cid, gid string) ([]Member, []*proto.RelationshipTuple, error) {
	rs, e := s.listRelationships(ctx, &proto.RelationshipFilter{Resource: &proto.Resource{Type: "group", Id: gid}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
	if e != nil {
		return nil, nil, e
	}
	out := []Member{}
	physical := []*proto.RelationshipTuple{}
	seen := map[string]struct{}{}
	for _, x := range rs {
		if x.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
			continue
		}
		t := x.GetTuple().GetTarget()
		if sub := t.GetSubject(); sub != nil && strings.HasPrefix(sub.Id, "user:") {
			users, err := s.db.ObjectStore(coredata.StoreSCIMResources).Index("by_client_core_user").GetAll(ctx, []any{cid, "User", strings.TrimPrefix(sub.Id, "user:")})
			if err != nil {
				return nil, nil, err
			}
			if len(users) != 1 {
				continue
			}
			physical = append(physical, x.GetTuple())
			u := users[0]
			member := Member{Value: recordString(u, "id"), Ref: s.baseURL + "/scim/v2/Users/" + recordString(u, "id"), Type: "User"}
			key := member.Type + "\x00" + member.Value
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, member)
		} else if ss := t.GetSubjectSet(); ss != nil && ss.Resource != nil {
			gr, e := s.findGroup(ctx, cid, ss.Resource.Id)
			if e != nil {
				if !errors.Is(e, idb.ErrNotFound) {
					return nil, nil, e
				}
				continue
			}
			physical = append(physical, x.GetTuple())
			member := Member{Value: gr.ID, Ref: s.baseURL + "/scim/v2/Groups/" + gr.ID, Type: "Group"}
			key := member.Type + "\x00" + member.Value
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, member)
		}
	}
	sortMembers(out)
	sort.SliceStable(physical, func(i, j int) bool { return physicalTupleKey(physical[i]) < physicalTupleKey(physical[j]) })
	return out, physical, nil
}

func sortMembers(members []Member) {
	sort.Slice(members, func(i, j int) bool {
		if members[i].Value != members[j].Value {
			return members[i].Value < members[j].Value
		}
		return members[i].Type < members[j].Type
	})
}
func (s *CompactService) groupTuple(ctx context.Context, cid, gid string, m Member) (*proto.RelationshipTuple, error) {
	if strings.TrimSpace(m.Value) == "" {
		return nil, invalid("group member value is required")
	}
	if u, e := s.findUser(ctx, cid, m.Value); e == nil {
		if err := validateMemberReference(m, "User", s.baseURL+"/scim/v2/Users/"+u.ID); err != nil {
			return nil, err
		}
		return &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + u.CoreUserID}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: gid}}, nil
	} else if !errors.Is(e, idb.ErrNotFound) {
		return nil, unavailable("could not validate group member")
	}
	if g, e := s.findGroup(ctx, cid, m.Value); e == nil {
		if err := validateMemberReference(m, "Group", s.baseURL+"/scim/v2/Groups/"+g.ID); err != nil {
			return nil, err
		}
		return &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: "group", Id: g.ID}, Relation: "member"}}}, Relation: "member", Resource: &proto.Resource{Type: "group", Id: gid}}, nil
	} else if !errors.Is(e, idb.ErrNotFound) {
		return nil, unavailable("could not validate group member")
	}
	return nil, noTarget("group member must reference a User or Group in this SCIM client")
}

func validateMemberReference(member Member, resourceType, expectedRef string) error {
	if member.Type != "" && !strings.EqualFold(strings.TrimSpace(member.Type), resourceType) {
		return invalid(fmt.Sprintf("group member type %q does not match %s", member.Type, resourceType))
	}
	if member.Ref != "" && strings.TrimSpace(member.Ref) != expectedRef {
		return invalid("group member $ref is immutable")
	}
	return nil
}
func (s *CompactService) tuplesForMembers(ctx context.Context, cid, gid string, ms []Member) ([]*proto.RelationshipTuple, error) {
	out := []*proto.RelationshipTuple{}
	seen := map[string]bool{}
	for _, m := range ms {
		t, e := s.groupTuple(ctx, cid, gid, m)
		if e != nil {
			return nil, e
		}
		if !seen[tupleKey(t)] {
			seen[tupleKey(t)] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return tupleKey(out[i]) < tupleKey(out[j]) })
	return out, nil
}

func (s *CompactService) actualGroupTuples(ctx context.Context, cid, gid string, members []Member) ([]*proto.RelationshipTuple, error) {
	desired, err := s.tuplesForMembers(ctx, cid, gid, members)
	if err != nil {
		return nil, err
	}
	logical := make(map[string]struct{}, len(desired))
	for _, tuple := range desired {
		logical[tupleKey(tuple)] = struct{}{}
	}
	relationships, err := s.listRelationships(ctx, &proto.RelationshipFilter{
		Resource: &proto.Resource{Type: "group", Id: gid}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
	}, 100)
	if err != nil {
		return nil, err
	}
	actual := make([]*proto.RelationshipTuple, 0, len(relationships))
	for _, relationship := range relationships {
		if relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME {
			continue
		}
		if _, ok := logical[tupleKey(relationship.GetTuple())]; ok {
			actual = append(actual, relationship.GetTuple())
		}
	}
	sort.SliceStable(actual, func(i, j int) bool { return physicalTupleKey(actual[i]) < physicalTupleKey(actual[j]) })
	return actual, nil
}

func (s *CompactService) CreateGroup(ctx context.Context, cid string, in groupInput) (*Group, error) {
	if err := validateResourceSchemas(in.Schemas, GroupSchemaURN); err != nil {
		return nil, err
	}
	if in.DisplayName == nil || strings.TrimSpace(*in.DisplayName) == "" {
		return nil, invalid("displayName is required")
	}
	now := s.nowUTC()
	g := storedResource{ID: uuid.NewString(), ClientID: cid, ResourceType: "Group", DisplayName: *in.DisplayName, CreatedAt: now, UpdatedAt: now}
	if in.ExternalID != nil {
		g.ExternalID = *in.ExternalID
	}
	ms := []Member{}
	if in.Members != nil {
		ms = *in.Members
	}
	tu, e := s.tuplesForMembers(ctx, cid, g.ID, ms)
	if e != nil {
		return nil, e
	}
	record, e := resourceRecord(g)
	if e != nil {
		return nil, unavailable("could not encode SCIM group")
	}
	if e = s.db.ObjectStore("scim_resources").Add(ctx, record); e != nil {
		if errors.Is(e, idb.ErrAlreadyExists) {
			return nil, conflict("SCIM group already exists")
		}
		return nil, unavailable("could not persist SCIM group")
	}
	if e = s.apply(ctx, nil, tu); e != nil {
		return nil, unavailable("could not apply group membership")
	}
	committed, e := s.findGroup(ctx, cid, g.ID)
	if e != nil {
		return nil, unavailable("could not reload SCIM group")
	}
	v, e := s.groupValue(ctx, committed)
	if e != nil {
		return nil, e
	}
	return &v, nil
}
func (s *CompactService) GetGroup(ctx context.Context, cid, id string) (*Group, error) {
	g, e := s.findGroup(ctx, cid, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return nil, notFoundResource("SCIM Group")
		}
		return nil, unavailable("could not load SCIM group")
	}
	v, e := s.groupValue(ctx, g)
	if e != nil {
		return nil, e
	}
	return &v, nil
}
func (s *CompactService) listGroups(ctx context.Context, cid, raw string, start, count int) (listResponse[Group], error) {
	clauses, e := parseGroupFilter(raw)
	if e != nil {
		return listResponse[Group]{}, invalidFilter(e.Error())
	}
	rs, e := s.listRecords(ctx, cid, "Group")
	if e != nil {
		return listResponse[Group]{}, unavailable("could not list SCIM groups")
	}
	sort.Slice(rs, func(i, j int) bool { return recordString(rs[i], "id") < recordString(rs[j], "id") })
	storedClauses := make([]groupFilterClause, 0, len(clauses))
	memberClauses := make([]groupFilterClause, 0, len(clauses))
	for _, clause := range clauses {
		if clause.attribute == "members.value" {
			memberClauses = append(memberClauses, clause)
		} else {
			storedClauses = append(storedClauses, clause)
		}
	}
	knownGroupIDs := make(map[string]struct{}, len(rs))
	for _, row := range rs {
		knownGroupIDs[recordString(row, "id")] = struct{}{}
	}
	allowedGroups := map[string]struct{}{}
	if len(memberClauses) > 0 {
		for i, clause := range memberClauses {
			target, ok, err := s.groupMemberTarget(ctx, cid, clause.value)
			if err != nil {
				return listResponse[Group]{}, unavailable("could not validate group member")
			}
			if !ok {
				return listResponse[Group]{Schemas: []string{ListSchemaURN}, TotalResults: 0, StartIndex: 1, ItemsPerPage: 0, Resources: []Group{}}, nil
			}
			relations, err := s.listRelationships(ctx, &proto.RelationshipFilter{Target: target, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
			if err != nil {
				return listResponse[Group]{}, unavailable("could not read group membership")
			}
			current := map[string]struct{}{}
			for _, relationship := range relations {
				if relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || !sameRelationshipTarget(relationship.GetTuple().GetTarget(), target) {
					continue
				}
				resource := relationship.GetTuple().GetResource()
				if resource.GetType() == "group" {
					if _, exists := knownGroupIDs[resource.GetId()]; exists {
						current[resource.GetId()] = struct{}{}
					}
				}
			}
			if i == 0 {
				allowedGroups = current
			} else {
				for groupID := range allowedGroups {
					if _, exists := current[groupID]; !exists {
						delete(allowedGroups, groupID)
					}
				}
			}
		}
	}
	matching := make([]idb.Record, 0, len(rs))
	for _, rr := range rs {
		g := stored(rr)
		if !matchesStoredGroupFilter(persistedGroup{ExternalID: g.ExternalID, DisplayName: g.DisplayName}, storedClauses) {
			continue
		}
		if len(memberClauses) > 0 {
			if _, ok := allowedGroups[g.ID]; !ok {
				continue
			}
		}
		matching = append(matching, rr)
	}
	if start > len(matching)+1 {
		start = len(matching) + 1
	}
	end := pageMin(start-1+count, len(matching))
	if end < start-1 {
		end = start - 1
	}
	page := matching[start-1 : end]
	all := make([]Group, 0, len(page))
	for _, rr := range page {
		v, err := s.groupValue(ctx, stored(rr))
		if err != nil {
			return listResponse[Group]{}, err
		}
		all = append(all, v)
	}
	return listResponse[Group]{Schemas: []string{ListSchemaURN}, TotalResults: len(matching), StartIndex: start, ItemsPerPage: len(all), Resources: all}, nil
}

func matchesStoredGroupFilter(group persistedGroup, clauses []groupFilterClause) bool {
	for _, clause := range clauses {
		switch clause.attribute {
		case "displayname":
			if normalize(group.DisplayName) != clause.value {
				return false
			}
		case "externalid":
			if group.ExternalID != clause.value {
				return false
			}
		}
	}
	return true
}

func (s *CompactService) groupMemberTarget(ctx context.Context, cid, value string) (*proto.RelationshipTarget, bool, error) {
	if user, err := s.findUser(ctx, cid, value); err == nil {
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + user.CoreUserID}}}, true, nil
	} else if !errors.Is(err, idb.ErrNotFound) {
		return nil, false, err
	}
	if group, err := s.findGroup(ctx, cid, value); err == nil {
		return &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: "group", Id: group.ID}, Relation: "member"}}}, true, nil
	} else if !errors.Is(err, idb.ErrNotFound) {
		return nil, false, err
	}
	return nil, false, nil
}
func (s *CompactService) mutateGroup(ctx context.Context, cid, id, ifm string, in groupInput) (*Group, error) {
	unlock := s.lock(id)
	defer unlock()
	r, e := s.findGroup(ctx, cid, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return nil, notFoundResource("SCIM Group")
		}
		return nil, unavailable("could not load SCIM group")
	}
	old, e := s.groupValue(ctx, r)
	if e != nil {
		return nil, e
	}
	return s.mutateGroupLocked(ctx, cid, id, ifm, r, old, in)
}

// mutateGroupLocked applies a mutation using the provider snapshot captured
// while the per-resource lock is held.
func (s *CompactService) mutateGroupLocked(ctx context.Context, cid, id, ifm string, snapshot storedResource, old Group, in groupInput) (*Group, error) {
	var e error
	next := persistedGroup{ExternalID: snapshot.ExternalID, DisplayName: snapshot.DisplayName, Members: old.Members}
	if in.ExternalID != nil {
		next.ExternalID = *in.ExternalID
	}
	if in.DisplayName != nil {
		next.DisplayName = *in.DisplayName
	}
	if in.Members != nil {
		next.Members = *in.Members
	}
	if strings.TrimSpace(next.DisplayName) == "" {
		return nil, invalid("displayName is required")
	}
	if e := validateRetainedMemberAttributes(old.Members, next.Members); e != nil {
		return nil, e
	}
	from, e := s.actualGroupTuples(ctx, cid, id, old.Members)
	if e != nil {
		return nil, e
	}
	to, e := s.tuplesForMembers(ctx, cid, id, next.Members)
	if e != nil {
		return nil, e
	}
	r := snapshot
	// The transaction serializes compact metadata across replicas; provider-
	// derived members remain outside it and require an atomic provider API for
	// fully conditional multi-relationship writes.
	tx, e := s.db.Transaction(ctx, []string{coredata.StoreSCIMResources}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if e != nil {
		return nil, unavailable("could not start SCIM transaction")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(ctx)
		}
	}()
	txRow, e := tx.ObjectStore(coredata.StoreSCIMResources).Get(ctx, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return nil, notFoundResource("SCIM Group")
		}
		return nil, unavailable("could not load SCIM group")
	}
	if recordString(txRow, "client_id") != cid || recordString(txRow, "resource_type") != "Group" {
		return nil, notFoundResource("SCIM Group")
	}
	r = stored(txRow)
	if ifm != "" && ifm != "*" && ifm != old.Meta.Version {
		return nil, &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	snapshotRecord, e := resourceRecord(snapshot)
	if e != nil {
		return nil, unavailable("could not encode SCIM group")
	}
	if !sameResourceContent(txRow, snapshotRecord) {
		if ifm == "" {
			return nil, unavailable("SCIM resource changed concurrently; retry the request")
		}
		return nil, &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	if next.ExternalID == snapshot.ExternalID && next.DisplayName == snapshot.DisplayName && sameTuples(from, to) {
		return &old, nil
	}
	r.ExternalID, r.DisplayName, r.UpdatedAt = next.ExternalID, next.DisplayName, s.nowUTC()
	record, e := resourceRecord(r)
	if e != nil {
		return nil, unavailable("could not encode SCIM group")
	}
	if e = tx.ObjectStore(coredata.StoreSCIMResources).Put(ctx, record); e != nil {
		return nil, unavailable("could not persist SCIM group")
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, unavailable("could not commit SCIM group")
	}
	committed = true
	if e = s.apply(ctx, from, to); e != nil {
		return nil, unavailable("could not apply group membership")
	}
	reloaded, e := s.findGroup(ctx, cid, id)
	if e != nil {
		return nil, unavailable("could not reload SCIM group")
	}
	v, e := s.groupValue(ctx, reloaded)
	if e != nil {
		return nil, e
	}
	return &v, nil
}

func sameTuples(a, b []*proto.RelationshipTuple) bool {
	left := make(map[string]struct{}, len(a))
	for _, tuple := range a {
		left[tupleKey(tuple)] = struct{}{}
	}
	right := make(map[string]struct{}, len(b))
	for _, tuple := range b {
		right[tupleKey(tuple)] = struct{}{}
	}
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}
func (s *CompactService) ReplaceGroup(ctx context.Context, cid, id, ifm string, in groupInput) (*Group, error) {
	if err := validateResourceSchemas(in.Schemas, GroupSchemaURN); err != nil {
		return nil, err
	}
	if in.DisplayName == nil {
		return nil, invalid("PUT requires displayName")
	}
	if in.Members == nil {
		empty := []Member{}
		in.Members = &empty
	}
	if in.ExternalID == nil {
		empty := ""
		in.ExternalID = &empty
	}
	return s.mutateGroup(ctx, cid, id, ifm, in)
}
func (s *CompactService) DeleteGroup(ctx context.Context, cid, id, ifm string) error {
	unlock := s.lock(id)
	defer unlock()
	r, e := s.findGroup(ctx, cid, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return notFoundResource("SCIM Group")
		}
		return unavailable("could not load SCIM group")
	}
	v, e := s.groupValue(ctx, r)
	if e != nil {
		return e
	}
	if ifm != "" && ifm != "*" && ifm != v.Meta.Version {
		return &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	// Revalidate compact metadata immediately before relationship cleanup. The
	// provider operation is outside this transaction, so cross-replica
	// conditional atomicity still depends on a provider transaction primitive.
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMResources}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return unavailable("could not start SCIM delete transaction")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(ctx)
		}
	}()
	current, err := tx.ObjectStore(coredata.StoreSCIMResources).Get(ctx, id)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return notFoundResource("SCIM Group")
		}
		return unavailable("could not revalidate SCIM group")
	}
	snapshotRecord, e := resourceRecord(r)
	if e != nil {
		return unavailable("could not encode SCIM group")
	}
	if !sameResourceContent(current, snapshotRecord) {
		if ifm == "" {
			return unavailable("SCIM resource changed concurrently; retry the request")
		}
		return &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("could not commit SCIM delete transaction")
	}
	committed = true
	if e = s.removeGroupRelationships(ctx, cid, id); e != nil {
		return unavailable("could not remove group relationships")
	}
	if e = s.db.ObjectStore("scim_resources").Delete(ctx, id); e != nil {
		return unavailable("could not delete SCIM group")
	}
	return nil
}

// removeGroupRelationships removes outgoing members and inbound nested-group
// references for exactly the group being deleted. Deleting the group makes
// both kinds of references invalid; unrelated resources are not inspected.
func (s *CompactService) removeGroupRelationships(ctx context.Context, clientID, groupID string) error {
	groups, err := s.listRecords(ctx, clientID, "Group")
	if err != nil {
		return err
	}
	knownGroups := make(map[string]struct{}, len(groups))
	for _, row := range groups {
		knownGroups[recordString(row, "id")] = struct{}{}
	}
	outgoing, err := s.listRelationships(ctx, &proto.RelationshipFilter{Resource: &proto.Resource{Type: "group", Id: groupID}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
	if err != nil {
		return err
	}
	target := &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{Resource: &proto.Resource{Type: "group", Id: groupID}, Relation: "member"}}}
	incoming, err := s.listRelationships(ctx, &proto.RelationshipFilter{Target: target, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
	if err != nil {
		return err
	}
	seen := map[string]*proto.RelationshipTuple{}
	for _, relationship := range outgoing {
		if relationship.GetSourceLayer() == proto.SourceLayer_SOURCE_LAYER_RUNTIME && relationship.GetTuple().GetResource().GetType() == "group" && relationship.GetTuple().GetResource().GetId() == groupID {
			seen[physicalTupleKey(relationship.GetTuple())] = relationship.GetTuple()
		}
	}
	for _, relationship := range incoming {
		if relationship.GetSourceLayer() != proto.SourceLayer_SOURCE_LAYER_RUNTIME || !sameRelationshipTarget(relationship.GetTuple().GetTarget(), target) {
			continue
		}
		tuple := relationship.GetTuple()
		if _, ok := knownGroups[tuple.GetResource().GetId()]; !ok {
			continue
		}
		seen[physicalTupleKey(tuple)] = tuple
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tuples := make([]*proto.RelationshipTuple, 0, len(keys))
	for _, key := range keys {
		tuples = append(tuples, seen[key])
	}
	return s.apply(ctx, tuples, nil)
}
