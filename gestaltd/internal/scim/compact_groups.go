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
	g := Group{Schemas: []string{GroupSchemaURN}, ID: r.ID, ExternalID: r.ExternalID, DisplayName: r.DisplayName, Meta: Meta{ResourceType: "Group", Created: r.CreatedAt, LastModified: r.UpdatedAt, Location: s.baseURL + "/scim/v2/Groups/" + r.ID}}
	m, e := s.liveMembers(ctx, r.ClientID, r.ID)
	if e != nil {
		return Group{}, unavailable("could not read group membership")
	}
	g.Members = m
	canon := struct {
		Schemas     []string `json:"schemas"`
		ID          string   `json:"id"`
		ExternalID  string   `json:"externalId,omitempty"`
		DisplayName string   `json:"displayName"`
		Members     []Member `json:"members,omitempty"`
	}{g.Schemas, g.ID, g.ExternalID, g.DisplayName, g.Members}
	g.Meta.Version = etag(canon)
	return g, nil
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

func (s *CompactService) liveMembers(ctx context.Context, cid, gid string) ([]Member, error) {
	rs, e := s.listRelationships(ctx, &proto.RelationshipFilter{Resource: &proto.Resource{Type: "group", Id: gid}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
	if e != nil {
		return nil, e
	}
	users, err := s.listRecords(ctx, cid, "User")
	if err != nil {
		return nil, err
	}
	byCore := map[string]idb.Record{}
	for _, u := range users {
		byCore[recordString(u, "core_user_id")] = u
	}
	out := []Member{}
	for _, x := range rs {
		t := x.GetTuple().GetTarget()
		if sub := t.GetSubject(); sub != nil && strings.HasPrefix(sub.Id, "user:") {
			u, ok := byCore[strings.TrimPrefix(sub.Id, "user:")]
			if !ok {
				continue
			}
			out = append(out, Member{Value: recordString(u, "id"), Ref: s.baseURL + "/scim/v2/Users/" + recordString(u, "id"), Type: "User"})
		} else if ss := t.GetSubjectSet(); ss != nil && ss.Resource != nil {
			gr, e := s.findGroup(ctx, cid, ss.Resource.Id)
			if e != nil {
				if !errors.Is(e, idb.ErrNotFound) {
					return nil, e
				}
				continue
			}
			out = append(out, Member{Value: gr.ID, Ref: s.baseURL + "/scim/v2/Groups/" + gr.ID, Type: "Group"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out, nil
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
	if e = s.db.ObjectStore("scim_resources").Add(ctx, resourceRecord(g)); e != nil {
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
	all := []Group{}
	for _, rr := range rs {
		g := stored(rr)
		v, e := s.groupValue(ctx, g)
		if e != nil {
			return listResponse[Group]{}, e
		}
		if matchesGroupFilter(persistedGroup{ExternalID: g.ExternalID, DisplayName: g.DisplayName, Members: v.Members}, clauses) {
			all = append(all, v)
		}
	}
	if start > len(all)+1 {
		start = len(all) + 1
	}
	end := pageMin(start-1+count, len(all))
	if end < start-1 {
		end = start - 1
	}
	n := end - start + 1
	if n < 0 {
		n = 0
	}
	return listResponse[Group]{Schemas: []string{ListSchemaURN}, TotalResults: len(all), StartIndex: start, ItemsPerPage: n, Resources: all[start-1 : end]}, nil
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
	from, e := s.tuplesForMembers(ctx, cid, id, old.Members)
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
	if !sameResourceContent(txRow, resourceRecord(snapshot)) {
		if ifm == "" {
			return nil, unavailable("SCIM resource changed concurrently; retry the request")
		}
		return nil, &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	if next.ExternalID == snapshot.ExternalID && next.DisplayName == snapshot.DisplayName && sameTuples(from, to) {
		return &old, nil
	}
	r.ExternalID, r.DisplayName, r.UpdatedAt = next.ExternalID, next.DisplayName, s.nowUTC()
	if e = tx.ObjectStore(coredata.StoreSCIMResources).Put(ctx, resourceRecord(r)); e != nil {
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
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, tuple := range a {
		seen[tupleKey(tuple)] = struct{}{}
	}
	for _, tuple := range b {
		if _, ok := seen[tupleKey(tuple)]; !ok {
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
	if !sameResourceContent(current, resourceRecord(r)) {
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
	outgoing, err := s.listRelationships(ctx, &proto.RelationshipFilter{Resource: &proto.Resource{Type: "group", Id: groupID}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
	if err != nil {
		return err
	}
	for _, relationship := range outgoing {
		if err := s.deleteRelationship(ctx, relationship.GetTuple()); err != nil {
			return err
		}
	}
	incoming, err := s.listRelationships(ctx, &proto.RelationshipFilter{Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
	if err != nil {
		return err
	}
	groups, err := s.listRecords(ctx, clientID, "Group")
	if err != nil {
		return err
	}
	knownGroups := make(map[string]struct{}, len(groups))
	for _, row := range groups {
		knownGroups[recordString(row, "id")] = struct{}{}
	}
	for _, relationship := range incoming {
		tuple := relationship.GetTuple()
		if _, ok := knownGroups[tuple.GetResource().GetId()]; !ok {
			continue
		}
		set := tuple.GetTarget().GetSubjectSet()
		if set == nil || set.GetResource().GetType() != "group" || set.GetResource().GetId() != groupID || set.GetRelation() != "member" {
			continue
		}
		if err := s.deleteRelationship(ctx, tuple); err != nil {
			return err
		}
	}
	return nil
}
