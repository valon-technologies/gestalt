package server_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// This file holds the single authorization evaluator the conformance suite in
// conformance_test.go runs every case against.
//
// HONEST PROVENANCE. The production relationship-graph evaluator lives outside
// this repository, in the gestalt-providers module
// github.com/valon-technologies/gestalt-providers/authorization/indexeddb. It
// is a separate Go module that is not in this repository's go.work, depends on
// the indexeddb host service at runtime, and would introduce a module cycle
// (it `replace`s the gestalt sdk and rpc modules back onto this checkout). CI
// runs `go test ./...` in the gestaltd module with no sibling checkout and no
// network, so that package cannot be built or run here.
//
// conformanceEvaluator is therefore a reference implementation of the documented
// evaluation semantics, transcribed from that provider's evaluateAccess:
//
//   - a resource type absent from the active model denies;
//   - an action is looked up by name, then by the "*" wildcard action;
//   - an action with no allowed relations denies;
//   - a relationship counts only when its relation is in the action's allowed
//     set and its target resolves to the subject;
//   - subject sets expand recursively with a visited set, so cycles terminate;
//   - matched relations are reported in the model's declared relation order;
//   - when nothing matched, the resource type's defaultRole allows if the
//     action permits it;
//   - malformed tuples are skipped rather than trusted.
//
// It is deliberately the ONLY evaluator the suite uses, reached through the
// single constructor newConformanceAuthorization. Swapping the real provider in
// is a change to that constructor alone: the suite is written against
// core.AuthorizationProvider and never against this type's internals.
//
// What this proves and does not prove is spelled out in the suite's header
// comment in conformance_test.go.

// conformanceWildcardAction is the model action name that matches every action.
const conformanceWildcardAction = "*"

// conformanceMaxTraversal bounds subject-set expansion work for one decision.
// A cyclic or adversarially wide relationship graph exhausts the budget and
// denies instead of running forever.
const conformanceMaxTraversal = 4096

// conformanceAction is one action a resource type answers about, with the
// relations that authorize it.
type conformanceAction struct {
	Name      string
	Relations []string
}

// conformanceResourceType is one resource type in the active model.
type conformanceResourceType struct {
	Name        string
	DefaultRole string
	Actions     []conformanceAction
}

// conformanceSpec is the authorization state one test case starts from.
type conformanceSpec struct {
	ResourceTypes []conformanceResourceType
	Relationships []*proto.Relationship
	// ListPageSize, when positive, makes ListRelationships page at that size so
	// a surface that reads relationships must follow next_page_token.
	ListPageSize int
}

// conformanceEvaluator is the shared reference evaluator. Every method is safe
// for concurrent use because httptest servers answer requests on their own
// goroutines.
type conformanceEvaluator struct {
	core.AuthorizationProvider

	mu            sync.Mutex
	resourceTypes []conformanceResourceType
	relationships []*proto.Relationship
	listPageSize  int

	checkAccessCalls     int
	checkAccessManyCalls int
	listCalls            int
	// budgetExhausted records that at least one decision hit the traversal
	// budget, which is how a test proves a cyclic graph stayed bounded.
	budgetExhausted bool
}

// newConformanceAuthorization is the suite's single evaluator constructor and
// the one place to swap in the real provider.
func newConformanceAuthorization(t *testing.T, spec conformanceSpec) *conformanceEvaluator {
	t.Helper()
	return &conformanceEvaluator{
		resourceTypes: spec.ResourceTypes,
		relationships: spec.Relationships,
		listPageSize:  spec.ListPageSize,
	}
}

func (e *conformanceEvaluator) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checkAccessCalls++
	return e.evaluateLocked(req), nil
}

func (e *conformanceEvaluator) CheckAccessMany(_ context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.checkAccessManyCalls++
	decisions := make([]*proto.CheckAccessResponse, 0, len(req.GetRequests()))
	for _, entry := range req.GetRequests() {
		decisions = append(decisions, e.evaluateLocked(entry))
	}
	return &proto.CheckAccessManyResponse{Decisions: decisions}, nil
}

// evaluateLocked is the evaluator itself. The single and batched RPCs both go
// through it, so a listing decision and an invocation decision cannot disagree.
func (e *conformanceEvaluator) evaluateLocked(req *proto.CheckAccessRequest) *proto.CheckAccessResponse {
	subjectID := strings.TrimSpace(req.GetSubject().GetId())
	actionName := strings.TrimSpace(req.GetAction().GetName())
	resource := req.GetResource()
	if subjectID == "" || actionName == "" || resource == nil {
		return &proto.CheckAccessResponse{}
	}

	resourceType, ok := e.findResourceTypeLocked(resource.GetType())
	if !ok {
		return &proto.CheckAccessResponse{}
	}
	action, ok := findConformanceAction(resourceType, actionName)
	if !ok {
		return &proto.CheckAccessResponse{}
	}
	allowed := make(map[string]struct{}, len(action.Relations))
	for _, relation := range action.Relations {
		if relation = strings.TrimSpace(relation); relation != "" {
			allowed[relation] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return &proto.CheckAccessResponse{}
	}

	budget := conformanceMaxTraversal
	matched := make(map[string]struct{}, len(allowed))
	for _, relationship := range e.relationships {
		tuple := relationship.GetTuple()
		if tuple == nil || !conformanceResourcesEqual(tuple.GetResource(), resource) {
			continue
		}
		relation := strings.TrimSpace(tuple.GetRelation())
		if _, ok := allowed[relation]; !ok {
			continue
		}
		if e.targetMatchesLocked(tuple.GetTarget(), subjectID, map[string]struct{}{}, &budget) {
			matched[relation] = struct{}{}
		}
	}
	if budget <= 0 {
		e.budgetExhausted = true
	}
	if len(matched) > 0 {
		return &proto.CheckAccessResponse{
			Allowed:          true,
			MatchedRelations: orderedConformanceRelations(action, matched),
		}
	}
	if defaultRole := strings.TrimSpace(resourceType.DefaultRole); defaultRole != "" {
		if _, ok := allowed[defaultRole]; ok {
			return &proto.CheckAccessResponse{Allowed: true, MatchedRelations: []string{defaultRole}}
		}
	}
	return &proto.CheckAccessResponse{}
}

// targetMatchesLocked resolves one relationship target to the subject. It
// terminates on cycles through visited and on runaway graphs through budget,
// and treats every malformed shape as "does not match" rather than as a match.
func (e *conformanceEvaluator) targetMatchesLocked(
	target *proto.RelationshipTarget,
	subjectID string,
	visited map[string]struct{},
	budget *int,
) bool {
	if target == nil || budget == nil {
		return false
	}
	*budget--
	if *budget <= 0 {
		return false
	}
	if subject := target.GetSubject(); subject != nil {
		return strings.TrimSpace(subject.GetId()) == subjectID
	}
	set := target.GetSubjectSet()
	if set == nil || set.GetResource() == nil {
		return false
	}
	setResource := set.GetResource()
	setRelation := strings.TrimSpace(set.GetRelation())
	if strings.TrimSpace(setResource.GetType()) == "" ||
		strings.TrimSpace(setResource.GetId()) == "" ||
		setRelation == "" {
		return false
	}
	key := setResource.GetType() + "\x00" + setResource.GetId() + "\x00" + setRelation
	if _, seen := visited[key]; seen {
		return false
	}
	visited[key] = struct{}{}

	for _, relationship := range e.relationships {
		tuple := relationship.GetTuple()
		if tuple == nil {
			continue
		}
		if strings.TrimSpace(tuple.GetRelation()) != setRelation {
			continue
		}
		if !conformanceResourcesEqual(tuple.GetResource(), setResource) {
			continue
		}
		if e.targetMatchesLocked(tuple.GetTarget(), subjectID, visited, budget) {
			return true
		}
	}
	return false
}

func (e *conformanceEvaluator) findResourceTypeLocked(name string) (conformanceResourceType, bool) {
	name = strings.TrimSpace(name)
	for _, resourceType := range e.resourceTypes {
		if strings.TrimSpace(resourceType.Name) == name {
			return resourceType, true
		}
	}
	return conformanceResourceType{}, false
}

func findConformanceAction(resourceType conformanceResourceType, name string) (conformanceAction, bool) {
	for _, action := range resourceType.Actions {
		if strings.TrimSpace(action.Name) == name {
			return action, true
		}
	}
	for _, action := range resourceType.Actions {
		if strings.TrimSpace(action.Name) == conformanceWildcardAction {
			return action, true
		}
	}
	return conformanceAction{}, false
}

// orderedConformanceRelations reports matched relations in the model's declared
// order, which is what makes the first reported relation deterministic.
func orderedConformanceRelations(action conformanceAction, matched map[string]struct{}) []string {
	out := make([]string, 0, len(matched))
	for _, relation := range action.Relations {
		relation = strings.TrimSpace(relation)
		if _, ok := matched[relation]; !ok {
			continue
		}
		out = append(out, relation)
		delete(matched, relation)
	}
	return out
}

func conformanceResourcesEqual(a, b *proto.Resource) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.TrimSpace(a.GetType()) == strings.TrimSpace(b.GetType()) &&
		strings.TrimSpace(a.GetId()) == strings.TrimSpace(b.GetId())
}

// ListRelationships answers roster reads. When the spec sets a page size it
// pages, so a caller that ignores next_page_token loses rows and the test
// notices.
func (e *conformanceEvaluator) ListRelationships(
	_ context.Context, req *proto.ListRelationshipsRequest,
) (*proto.ListRelationshipsResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listCalls++

	filtered := make([]*proto.Relationship, 0, len(e.relationships))
	for _, relationship := range e.relationships {
		if conformanceMatchesFilter(relationship, req.GetFilter()) {
			filtered = append(filtered, relationship)
		}
	}

	pageSize := e.listPageSize
	if pageSize <= 0 || pageSize >= len(filtered) {
		return &proto.ListRelationshipsResponse{Relationships: filtered}, nil
	}
	offset := 0
	if token := strings.TrimSpace(req.GetPageToken()); token != "" {
		parsed, err := strconv.Atoi(token)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid page token %q", token)
		}
		offset = parsed
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := min(offset+pageSize, len(filtered))
	resp := &proto.ListRelationshipsResponse{Relationships: filtered[offset:end]}
	if end < len(filtered) {
		resp.NextPageToken = strconv.Itoa(end)
	}
	return resp, nil
}

func conformanceMatchesFilter(relationship *proto.Relationship, filter *proto.RelationshipFilter) bool {
	tuple := relationship.GetTuple()
	if tuple == nil {
		return false
	}
	if filter == nil {
		return true
	}
	if resource := filter.GetResource(); resource != nil && !conformanceResourcesEqual(tuple.GetResource(), resource) {
		return false
	}
	if relation := strings.TrimSpace(filter.GetRelation()); relation != "" &&
		strings.TrimSpace(tuple.GetRelation()) != relation {
		return false
	}
	if target := filter.GetTarget().GetSubject(); target != nil {
		subject := tuple.GetTarget().GetSubject()
		if subject == nil || strings.TrimSpace(subject.GetId()) != strings.TrimSpace(target.GetId()) {
			return false
		}
	}
	return true
}

// AddRelationship and DeleteRelationship let a test grant and then revoke access
// against the same live evaluator the server is already talking to.
func (e *conformanceEvaluator) AddRelationship(
	_ context.Context, req *proto.AddRelationshipRequest,
) (*proto.AddRelationshipResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if req.GetRelationship().GetTuple() == nil {
		return nil, fmt.Errorf("relationship tuple is required")
	}
	e.relationships = append(e.relationships, req.GetRelationship())
	return &proto.AddRelationshipResponse{}, nil
}

func (e *conformanceEvaluator) DeleteRelationship(
	_ context.Context, req *proto.DeleteRelationshipRequest,
) (*proto.DeleteRelationshipResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	want := req.GetRelationshipTuple()
	if want == nil {
		return nil, fmt.Errorf("relationship tuple is required")
	}
	kept := make([]*proto.Relationship, 0, len(e.relationships))
	for _, relationship := range e.relationships {
		if conformanceTupleEqual(relationship.GetTuple(), want) {
			continue
		}
		kept = append(kept, relationship)
	}
	e.relationships = kept
	return &proto.DeleteRelationshipResponse{}, nil
}

func conformanceTupleEqual(a, b *proto.RelationshipTuple) bool {
	if a == nil || b == nil {
		return false
	}
	if strings.TrimSpace(a.GetRelation()) != strings.TrimSpace(b.GetRelation()) {
		return false
	}
	if !conformanceResourcesEqual(a.GetResource(), b.GetResource()) {
		return false
	}
	aSubject, bSubject := a.GetTarget().GetSubject(), b.GetTarget().GetSubject()
	if aSubject != nil && bSubject != nil {
		return strings.TrimSpace(aSubject.GetId()) == strings.TrimSpace(bSubject.GetId())
	}
	aSet, bSet := a.GetTarget().GetSubjectSet(), b.GetTarget().GetSubjectSet()
	if aSet != nil && bSet != nil {
		return conformanceResourcesEqual(aSet.GetResource(), bSet.GetResource()) &&
			strings.TrimSpace(aSet.GetRelation()) == strings.TrimSpace(bSet.GetRelation())
	}
	return false
}

func (e *conformanceEvaluator) ListActiveModelResourceTypes(
	_ context.Context, req *proto.ListActiveModelResourceTypesRequest,
) (*proto.ListActiveModelResourceTypesResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	name := strings.TrimSpace(req.GetFilter().GetName())
	out := []*proto.AuthorizationModelResourceType{}
	for _, resourceType := range e.resourceTypes {
		if name != "" && strings.TrimSpace(resourceType.Name) != name {
			continue
		}
		entry := &proto.AuthorizationModelResourceType{
			Name:        resourceType.Name,
			DefaultRole: resourceType.DefaultRole,
		}
		for _, action := range resourceType.Actions {
			entry.Actions = append(entry.Actions, &proto.ModelAction{
				Name:      action.Name,
				Relations: action.Relations,
			})
		}
		out = append(out, entry)
	}
	return &proto.ListActiveModelResourceTypesResponse{ResourceTypes: out}, nil
}

func (e *conformanceEvaluator) Ping(context.Context) error { return nil }

func (e *conformanceEvaluator) Close() error { return nil }

// counters snapshots the call counters without racing the server goroutines.
func (e *conformanceEvaluator) counters() (checkAccess, checkAccessMany, list int, budgetExhausted bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.checkAccessCalls, e.checkAccessManyCalls, e.listCalls, e.budgetExhausted
}

// grant adds a runtime relationship the way a live grant would.
func (e *conformanceEvaluator) grant(t *testing.T, relationship *proto.Relationship) {
	t.Helper()
	if _, err := e.AddRelationship(
		context.Background(), &proto.AddRelationshipRequest{Relationship: relationship},
	); err != nil {
		t.Fatalf("AddRelationship: %v", err)
	}
}

// revoke removes a runtime relationship the way a live revoke would.
func (e *conformanceEvaluator) revoke(t *testing.T, relationship *proto.Relationship) {
	t.Helper()
	if _, err := e.DeleteRelationship(
		context.Background(), &proto.DeleteRelationshipRequest{RelationshipTuple: relationship.GetTuple()},
	); err != nil {
		t.Fatalf("DeleteRelationship: %v", err)
	}
}

// --- relationship builders -------------------------------------------------

// conformanceDirectGrant is a plain subject -> resource grant.
func conformanceDirectGrant(subjectID, relation, resourceType, resourceID string) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: subjectID}},
			},
			Relation: relation,
			Resource: &proto.Resource{Type: resourceType, Id: resourceID},
		},
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
	}
}

// conformanceGroupMember makes a subject a member of a group.
func conformanceGroupMember(subjectID, groupID string) *proto.Relationship {
	return conformanceDirectGrant(subjectID, "member", "group", groupID)
}

// conformanceNestedGroupMember makes every member of child a member of parent,
// which is the one extra hop that turns a one-level grant into a nested one.
func conformanceNestedGroupMember(childGroupID, parentGroupID string) *proto.Relationship {
	return conformanceSubjectSetGrant("group", childGroupID, "member", "member", "group", parentGroupID)
}

// conformanceGroupGrant grants a role on a resource to a group's members.
func conformanceGroupGrant(groupID, relation, resourceType, resourceID string) *proto.Relationship {
	return conformanceSubjectSetGrant("group", groupID, "member", relation, resourceType, resourceID)
}

// conformanceSubjectSetGrant is the general subject-set tuple:
// (setType:setID#setRelation) holds `relation` on resourceType:resourceID.
func conformanceSubjectSetGrant(
	setType, setID, setRelation, relation, resourceType, resourceID string,
) *proto.Relationship {
	return &proto.Relationship{
		Tuple: &proto.RelationshipTuple{
			Target: &proto.RelationshipTarget{
				Kind: &proto.RelationshipTarget_SubjectSet{SubjectSet: &proto.SubjectSet{
					Resource: &proto.Resource{Type: setType, Id: setID},
					Relation: setRelation,
				}},
			},
			Relation: relation,
			Resource: &proto.Resource{Type: resourceType, Id: resourceID},
		},
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME,
	}
}
