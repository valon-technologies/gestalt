package scim

// This file is the live SCIM implementation. It intentionally stores only
// provider-independent metadata; authorization relationships are the source of
// truth for active state and membership.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
	coredb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type compactProjection struct{ relation, resourceType, resourceID string }
type compactClient struct {
	id          string
	domains     map[string]struct{}
	projections []compactProjection
}
type compactCredential struct {
	token  string
	client *compactClient
}
type resourceLock struct{ mu sync.Mutex }

var compactLocks sync.Map

type storedResource struct {
	ID, ClientID, ResourceType, ExternalID, CoreUserID, UserName, DisplayName string
	Profile                                                                   userProfile
	CreatedAt, UpdatedAt                                                      time.Time
}
type userProfile struct {
	Name   Name    `json:"name,omitempty"`
	Emails []Email `json:"emails,omitempty"`
}

type CompactService struct {
	db            coredb.IndexedDB
	authorization core.AuthorizationProvider
	baseURL       string
	clients       map[string]*compactClient
	credentials   []compactCredential
	domainOwners  map[string]string
	now           func() time.Time
}

func NewService(db coredb.IndexedDB, authorization core.AuthorizationProvider, baseURL string, cfg config.ServerSCIMConfig) (*Service, error) {
	s := &CompactService{db: db, authorization: authorization, baseURL: strings.TrimRight(baseURL, "/"), clients: map[string]*compactClient{}, domainOwners: map[string]string{}, now: time.Now}
	if _, err := db.CreateObjectStore(context.Background(), coredata.StoreSCIMResources, coredata.SCIMResourcesSchema); err != nil && !errors.Is(err, idb.ErrAlreadyExists) {
		return nil, fmt.Errorf("create scim_resources: %w", err)
	}
	for id, cc := range cfg.Clients {
		c := &compactClient{id: id, domains: map[string]struct{}{}}
		for _, d := range cc.AuthoritativeUserDomains {
			d = normalize(d)
			c.domains[d] = struct{}{}
			s.domainOwners[d] = id
		}
		for _, p := range cc.ActiveUserRelationships {
			c.projections = append(c.projections, compactProjection{strings.TrimSpace(p.Relation), strings.TrimSpace(p.Resource.Type), strings.TrimSpace(p.Resource.ID)})
		}
		sort.Slice(c.projections, func(i, j int) bool { return projectionKey(c.projections[i]) < projectionKey(c.projections[j]) })
		s.clients[id] = c
		for _, cred := range cc.Credentials {
			s.credentials = append(s.credentials, compactCredential{strings.TrimSpace(cred.BearerToken), c})
		}
	}
	if authorization == nil {
		for _, client := range s.clients {
			if len(client.projections) > 0 || len(client.domains) > 0 {
				return nil, fmt.Errorf("SCIM authorization provider is required when authoritative domains or activeUserRelationships are configured")
			}
		}
	}
	return &Service{compact: s}, nil
}
func projectionKey(p compactProjection) string {
	return p.resourceType + "\x00" + p.resourceID + "\x00" + p.relation
}
func (s *CompactService) Enabled() bool { return s != nil && len(s.clients) > 0 }
func (s *CompactService) ClientForToken(token string) (string, bool) {
	for _, c := range s.credentials {
		if len(token) == len(c.token) && subtle.ConstantTimeCompare([]byte(token), []byte(c.token)) == 1 {
			return c.client.id, true
		}
	}
	return "", false
}
func (s *CompactService) Start(context.Context) {}
func (s *CompactService) lock(id string) func() {
	v, _ := compactLocks.LoadOrStore(id, &resourceLock{})
	l := v.(*resourceLock)
	l.mu.Lock()
	return l.mu.Unlock
}
func (s *CompactService) nowUTC() time.Time { return s.now().UTC().Truncate(time.Millisecond) }

func (s *CompactService) get(ctx context.Context, clientID, id, typ string) (idb.Record, error) {
	r, e := s.db.ObjectStore(coredata.StoreSCIMResources).Get(ctx, id)
	if e != nil {
		return nil, e
	}
	if recordString(r, "client_id") != clientID || recordString(r, "resource_type") != typ {
		return nil, idb.ErrNotFound
	}
	return r, nil
}
func (s *CompactService) listRecords(ctx context.Context, clientID, typ string) ([]idb.Record, error) {
	return s.db.ObjectStore(coredata.StoreSCIMResources).Index("by_client_type").GetAll(ctx, []any{clientID, typ})
}

type recordStore interface {
	GetAll(context.Context, any, ...uint32) ([]idb.Record, error)
}

func coreUserReferencedByOtherClientStore(ctx context.Context, store recordStore, coreID, clientID, resourceID string) (bool, error) {
	rows, err := store.GetAll(ctx, nil)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if recordString(row, "resource_type") == "User" && recordString(row, "core_user_id") == coreID &&
			(recordString(row, "client_id") != clientID || recordString(row, "id") != resourceID) {
			return true, nil
		}
	}
	return false, nil
}
func stored(r idb.Record) storedResource {
	var p userProfile
	_ = decodeJSONValue(r["profile"], &p)
	return storedResource{ID: recordString(r, "id"), ClientID: recordString(r, "client_id"), ResourceType: recordString(r, "resource_type"), ExternalID: recordString(r, "external_id"), CoreUserID: recordString(r, "core_user_id"), UserName: recordString(r, "user_name"), Profile: p, DisplayName: recordString(r, "display_name"), CreatedAt: recordTime(r, "created_at"), UpdatedAt: recordTime(r, "updated_at")}
}
func resourceRecord(x storedResource) (idb.Record, error) {
	r := idb.Record{"id": x.ID, "client_id": x.ClientID, "resource_type": x.ResourceType, "created_at": x.CreatedAt, "updated_at": x.UpdatedAt}
	if x.ExternalID != "" {
		r["external_id"] = x.ExternalID
	}
	if x.ResourceType == "User" {
		if x.Profile.Name != (Name{}) || len(x.Profile.Emails) > 0 {
			// IndexedDB's wire codec accepts JSON-native maps/slices, not
			// json.RawMessage. Round-trip the profile through JSON so the
			// stored value has the same shape while remaining codec-compatible.
			p, err := json.Marshal(x.Profile)
			if err != nil {
				return nil, fmt.Errorf("marshal profile: %w", err)
			}
			var native any
			if err := json.Unmarshal(p, &native); err != nil {
				return nil, fmt.Errorf("normalize profile: %w", err)
			}
			r["profile"] = native
		}
	}
	if x.ResourceType == "Group" {
		r["display_name"] = x.DisplayName
	}
	if x.CoreUserID != "" {
		r["core_user_id"] = x.CoreUserID
	}
	if x.UserName != "" {
		r["user_name"] = x.UserName
	}
	return r, nil
}

// sameResourceContent compares the compact fields represented by SCIM. The
// weak ETag is content-derived, so timestamp-only touchRows updates must not
// turn a still-valid conditional request into a snapshot conflict.
func sameResourceContent(a, b idb.Record) bool {
	for _, key := range []string{"id", "client_id", "resource_type", "external_id", "core_user_id", "user_name", "display_name"} {
		if recordString(a, key) != recordString(b, key) {
			return false
		}
	}
	if a["profile"] == nil && b["profile"] == nil {
		return true
	}
	var ap, bp any
	if err := decodeJSONValue(a["profile"], &ap); err != nil {
		return false
	}
	if err := decodeJSONValue(b["profile"], &bp); err != nil {
		return false
	}
	return reflect.DeepEqual(ap, bp)
}
func (s *CompactService) findUser(ctx context.Context, clientID, id string) (storedResource, error) {
	r, e := s.get(ctx, clientID, id, "User")
	if e != nil {
		return storedResource{}, e
	}
	return stored(r), nil
}
func (s *CompactService) findGroup(ctx context.Context, clientID, id string) (storedResource, error) {
	r, e := s.get(ctx, clientID, id, "Group")
	if e != nil {
		return storedResource{}, e
	}
	return stored(r), nil
}
func (s *CompactService) emailFor(u persistedUser) (string, error) {
	if len(u.Emails) > 0 {
		for _, e := range u.Emails {
			if e.Primary && strings.EqualFold(strings.TrimSpace(e.Type), "work") {
				return normalize(e.Value), nil
			}
		}
		for _, e := range u.Emails {
			if e.Primary && strings.TrimSpace(e.Value) != "" {
				return normalize(e.Value), nil
			}
		}
		for _, e := range u.Emails {
			if strings.EqualFold(strings.TrimSpace(e.Type), "work") && strings.TrimSpace(e.Value) != "" {
				return normalize(e.Value), nil
			}
		}
	}
	if m, err := mail.ParseAddress(u.UserName); err == nil && normalize(m.Address) == normalize(u.UserName) {
		return normalize(m.Address), nil
	}
	if strings.Contains(u.UserName, "@") {
		return normalize(u.UserName), nil
	}
	return "", invalid("user must include an email address")
}
func (s *CompactService) userValue(ctx context.Context, r storedResource) (User, error) {
	cu, e := coredata.NewUserService(s.db).GetUser(ctx, r.CoreUserID)
	if e != nil {
		return User{}, unavailable("could not load linked user")
	}
	lastModified := r.UpdatedAt
	if cu.UpdatedAt.After(lastModified) {
		lastModified = cu.UpdatedAt
	}
	u := User{Schemas: []string{UserSchemaURN}, ID: r.ID, ExternalID: r.ExternalID, UserName: r.UserName, DisplayName: cu.DisplayName, Name: r.Profile.Name, Emails: r.Profile.Emails, Meta: Meta{ResourceType: "User", Created: r.CreatedAt, LastModified: lastModified, Location: s.baseURL + "/scim/v2/Users/" + r.ID}}
	u.Active, e = s.userActive(ctx, r)
	if e != nil {
		return User{}, unavailable("could not read authorization state")
	}
	u.Groups, e = s.userGroupsNested(ctx, r.ClientID, r.CoreUserID)
	if e != nil {
		return User{}, unavailable("could not read group membership")
	}
	canon := struct {
		Schemas     []string   `json:"schemas"`
		ID          string     `json:"id"`
		ExternalID  string     `json:"externalId,omitempty"`
		UserName    string     `json:"userName"`
		Active      bool       `json:"active"`
		DisplayName string     `json:"displayName,omitempty"`
		Name        Name       `json:"name,omitempty"`
		Emails      []Email    `json:"emails,omitempty"`
		Groups      []GroupRef `json:"groups,omitempty"`
	}{u.Schemas, u.ID, u.ExternalID, u.UserName, u.Active, u.DisplayName, u.Name, u.Emails, u.Groups}
	u.Meta.Version = etag(canon)
	return u, nil
}
func (s *CompactService) userActive(ctx context.Context, r storedResource) (bool, error) {
	if r.CoreUserID == "" {
		return false, nil
	}
	c := s.clients[r.ClientID]
	if c == nil {
		return false, nil
	}
	for _, p := range c.projections {
		ok, e := s.hasTuple(ctx, userTuple(r.CoreUserID, p))
		if e != nil {
			return false, e
		}
		if !ok {
			return false, nil
		}
	}
	return len(c.projections) > 0, nil
}
func userTuple(uid string, p compactProjection) *proto.RelationshipTuple {
	return &proto.RelationshipTuple{Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{Type: "subject", Id: "user:" + uid}}}, Relation: p.relation, Resource: &proto.Resource{Type: p.resourceType, Id: p.resourceID}}
}
func (s *CompactService) hasTuple(ctx context.Context, t *proto.RelationshipTuple) (bool, error) {
	r, e := s.listRelationships(ctx, &proto.RelationshipFilter{Target: t.Target, Relation: t.Relation, Resource: t.Resource, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 2)
	if e != nil {
		return false, e
	}
	return len(r) > 0, nil
}

func (s *CompactService) listRelationships(ctx context.Context, filter *proto.RelationshipFilter, pageSize int32) ([]*proto.Relationship, error) {
	if s.authorization == nil {
		return nil, errors.New("authorization provider is unavailable")
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	var all []*proto.Relationship
	var token string
	for {
		response, err := s.authorization.ListRelationships(ctx, &proto.ListRelationshipsRequest{Filter: filter, PageSize: pageSize, PageToken: token})
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("authorization provider returned an empty relationship response")
		}
		all = append(all, response.Relationships...)
		if response.NextPageToken == "" {
			return all, nil
		}
		if response.NextPageToken == token {
			return nil, errors.New("authorization provider returned a repeated relationship page token")
		}
		token = response.NextPageToken
	}
}

func (s *CompactService) relationshipPresent(ctx context.Context, tuple *proto.RelationshipTuple) (bool, error) {
	if tuple == nil {
		return false, nil
	}
	relationships, err := s.listRelationships(ctx, &proto.RelationshipFilter{Target: tuple.Target, Relation: tuple.Relation, Resource: tuple.Resource, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 2)
	return len(relationships) > 0, err
}

// touchRelationship updates only metadata whose public representation can be
// affected by a successful shared authorization write. The provider remains
// canonical; this timestamp is informational and is never used for ETags.
func (s *CompactService) touchRelationship(ctx context.Context, tuple *proto.RelationshipTuple) error {
	if tuple == nil || tuple.GetTarget() == nil {
		return nil
	}
	rows, err := s.db.ObjectStore(coredata.StoreSCIMResources).GetAll(ctx, nil)
	if err != nil {
		return err
	}
	touch := map[string]struct{}{}
	for _, row := range rows {
		if recordString(row, "resource_type") != "User" || recordString(row, "core_user_id") == "" {
			continue
		}
		clientID := recordString(row, "client_id")
		if subject := tuple.GetTarget().GetSubject(); subject != nil && subject.GetId() == "user:"+recordString(row, "core_user_id") {
			inGroup, err := s.clientHasGroup(ctx, clientID, tuple.GetResource().GetType(), tuple.GetResource().GetId())
			if err != nil {
				return err
			}
			if s.isConfiguredProjection(clientID, tuple) || inGroup {
				touch[recordString(row, "id")] = struct{}{}
			}
		}
	}
	if tuple.GetResource().GetType() == "group" && tuple.GetRelation() == "member" {
		for _, row := range rows {
			if recordString(row, "resource_type") == "Group" && recordString(row, "id") == tuple.GetResource().GetId() {
				touch[recordString(row, "id")] = struct{}{}
			}
		}
		for _, row := range rows {
			if recordString(row, "resource_type") != "Group" || recordString(row, "id") != tuple.GetResource().GetId() {
				continue
			}
			clientID := recordString(row, "client_id")
			memberUsers, err := s.affectedGroupMemberUsers(ctx, clientID, tuple)
			if err != nil {
				return err
			}
			for _, coreID := range memberUsers {
				for _, userRow := range rows {
					if recordString(userRow, "resource_type") == "User" && recordString(userRow, "client_id") == clientID && recordString(userRow, "core_user_id") == coreID {
						touch[recordString(userRow, "id")] = struct{}{}
					}
				}
			}
		}
	}
	if len(touch) == 0 {
		return nil
	}
	return s.touchRows(ctx, touch)
}

// touchRows performs read-modify-write in one transaction so an external
// authorization update cannot overwrite a concurrent SCIM profile change with
// a stale whole row.
func (s *CompactService) touchRows(ctx context.Context, ids map[string]struct{}) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Transaction(ctx, []string{coredata.StoreSCIMResources}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(ctx)
		}
	}()
	store := tx.ObjectStore(coredata.StoreSCIMResources)
	for id := range ids {
		row, err := store.Get(ctx, id)
		if err != nil {
			if errors.Is(err, idb.ErrNotFound) {
				continue
			}
			return err
		}
		now := s.nowUTC()
		if previous := recordTime(row, "updated_at"); !previous.IsZero() && !now.After(previous) {
			now = previous.Add(time.Millisecond)
		}
		row["updated_at"] = now
		if err := store.Put(ctx, row); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *CompactService) isConfiguredProjection(clientID string, tuple *proto.RelationshipTuple) bool {
	client := s.clients[clientID]
	if client == nil {
		return false
	}
	for _, projection := range client.projections {
		if projection.resourceType == tuple.GetResource().GetType() && projection.resourceID == tuple.GetResource().GetId() && projection.relation == tuple.GetRelation() {
			return true
		}
	}
	return false
}

func (s *CompactService) clientHasGroup(ctx context.Context, clientID, resourceType, resourceID string) (bool, error) {
	if resourceType != "group" {
		return false, nil
	}
	rows, err := s.listRecords(ctx, clientID, "Group")
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if recordString(row, "id") == resourceID {
			return true, nil
		}
	}
	return false, nil
}

func (s *CompactService) affectedGroupMemberUsers(ctx context.Context, clientID string, tuple *proto.RelationshipTuple) ([]string, error) {
	groupTarget := tuple.GetTarget().GetSubjectSet()
	if groupTarget == nil || groupTarget.GetResource().GetType() != "group" {
		if subject := tuple.GetTarget().GetSubject(); subject != nil && strings.HasPrefix(subject.GetId(), "user:") {
			return []string{strings.TrimPrefix(subject.GetId(), "user:")}, nil
		}
		return nil, nil
	}
	start := groupTarget.GetResource().GetId()
	users := map[string]struct{}{}
	queue := []string{start}
	seen := map[string]struct{}{}
	for len(queue) > 0 {
		gid := queue[0]
		queue = queue[1:]
		if _, ok := seen[gid]; ok {
			continue
		}
		seen[gid] = struct{}{}
		rels, err := s.listRelationships(ctx, &proto.RelationshipFilter{Resource: &proto.Resource{Type: "group", Id: gid}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			target := rel.GetTuple().GetTarget()
			if subject := target.GetSubject(); subject != nil && strings.HasPrefix(subject.GetId(), "user:") {
				users[strings.TrimPrefix(subject.GetId(), "user:")] = struct{}{}
			}
			if set := target.GetSubjectSet(); set != nil && set.GetResource().GetType() == "group" {
				queue = append(queue, set.GetResource().GetId())
			}
		}
	}
	out := make([]string, 0, len(users))
	for id := range users {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}
func (s *CompactService) apply(ctx context.Context, from, to []*proto.RelationshipTuple) error {
	// AuthorizationProvider exposes only one-tuple Add/Delete operations. A
	// remote provider therefore cannot make this multi-tuple diff atomic. We
	// apply it deterministically and return an error at the first failure; the
	// SCIM caller sees 503 and retries against the provider's live state. This
	// is an explicit RFC 7644 §3.5.2 compliance limitation until the provider
	// API offers a transactional multi-relationship primitive.
	fm := map[string]*proto.RelationshipTuple{}
	tm := map[string]*proto.RelationshipTuple{}
	for _, t := range from {
		fm[tupleKey(t)] = t
	}
	for _, t := range to {
		tm[tupleKey(t)] = t
	}
	keys := []string{}
	for k := range fm {
		if _, ok := tm[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if e := s.deleteRelationship(ctx, fm[k]); e != nil {
			return e
		}
	}
	keys = nil
	for k := range tm {
		if _, ok := fm[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if e := s.addRelationship(ctx, tm[k]); e != nil {
			return e
		}
	}
	return nil
}

func (s *CompactService) addRelationship(ctx context.Context, tuple *proto.RelationshipTuple) error {
	_, err := s.authorization.AddRelationship(ctx, &proto.AddRelationshipRequest{Relationship: &proto.Relationship{Tuple: tuple, SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}})
	if isAlreadyExistsError(err) {
		return s.touchRelationship(ctx, tuple)
	}
	if err != nil {
		return err
	}
	return s.touchRelationship(ctx, tuple)
}

func (s *CompactService) deleteRelationship(ctx context.Context, tuple *proto.RelationshipTuple) error {
	var affected []string
	if tuple != nil && tuple.GetTarget().GetSubjectSet() != nil {
		var err error
		affected, err = s.affectedGroupMemberUsers(ctx, "", tuple)
		if err != nil {
			return err
		}
	}
	_, err := s.authorization.DeleteRelationship(ctx, &proto.DeleteRelationshipRequest{RelationshipTuple: tuple})
	if isNotFoundError(err) {
		if err := s.touchRelationship(ctx, tuple); err != nil {
			return err
		}
		return s.touchCoreUsers(ctx, affected)
	}
	if err != nil {
		return err
	}
	if err := s.touchRelationship(ctx, tuple); err != nil {
		return err
	}
	return s.touchCoreUsers(ctx, affected)
}

func isAlreadyExistsError(err error) bool {
	return errors.Is(err, core.ErrAlreadyExists) || errors.Is(err, idb.ErrAlreadyExists) || status.Code(err) == codes.AlreadyExists
}

func isNotFoundError(err error) bool {
	return errors.Is(err, core.ErrNotFound) || errors.Is(err, idb.ErrNotFound) || status.Code(err) == codes.NotFound
}

func (s *CompactService) touchCoreUsers(ctx context.Context, coreIDs []string) error {
	if len(coreIDs) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(coreIDs))
	for _, id := range coreIDs {
		wanted[id] = struct{}{}
	}
	rows, err := s.db.ObjectStore(coredata.StoreSCIMResources).GetAll(ctx, nil)
	if err != nil {
		return err
	}
	ids := make(map[string]struct{})
	for _, row := range rows {
		if recordString(row, "resource_type") == "User" {
			if _, ok := wanted[recordString(row, "core_user_id")]; ok {
				ids[recordString(row, "id")] = struct{}{}
			}
		}
	}
	return s.touchRows(ctx, ids)
}

// touchCoreUsersByClient limits relationship-driven timestamp updates to the
// SCIM namespaces whose public representation can actually change.
func (s *CompactService) touchCoreUsersByClient(ctx context.Context, affected map[string]map[string]struct{}) error {
	if len(affected) == 0 {
		return nil
	}
	rows, err := s.db.ObjectStore(coredata.StoreSCIMResources).GetAll(ctx, nil)
	if err != nil {
		return err
	}
	ids := make(map[string]struct{})
	for _, row := range rows {
		if recordString(row, "resource_type") != "User" {
			continue
		}
		clientUsers := affected[recordString(row, "client_id")]
		if _, ok := clientUsers[recordString(row, "core_user_id")]; ok {
			ids[recordString(row, "id")] = struct{}{}
		}
	}
	return s.touchRows(ctx, ids)
}

// captureRelationshipAffectedUsers records the users whose SCIM groups or
// active projection may be changed by tuple before that tuple is deleted or a
// provider state replacement runs. The provider is canonical, so this is
// deliberately an in-memory set used only for timestamp maintenance.
func (s *CompactService) captureRelationshipAffectedUsers(ctx context.Context, tuple *proto.RelationshipTuple, affected map[string]map[string]struct{}) error {
	if tuple == nil || tuple.GetTarget() == nil || tuple.GetResource() == nil {
		return nil
	}
	rows, err := s.db.ObjectStore(coredata.StoreSCIMResources).GetAll(ctx, nil)
	if err != nil {
		return err
	}
	mark := func(clientID, coreID string) {
		if affected[clientID] == nil {
			affected[clientID] = map[string]struct{}{}
		}
		affected[clientID][coreID] = struct{}{}
	}
	if subject := tuple.GetTarget().GetSubject(); subject != nil && strings.HasPrefix(subject.GetId(), "user:") {
		coreID := strings.TrimPrefix(subject.GetId(), "user:")
		for _, row := range rows {
			if recordString(row, "resource_type") != "User" || recordString(row, "core_user_id") != coreID {
				continue
			}
			clientID := recordString(row, "client_id")
			relevant := s.isConfiguredProjection(clientID, tuple)
			if !relevant && tuple.GetResource().GetType() == "group" && tuple.GetRelation() == "member" {
				for _, group := range rows {
					if recordString(group, "resource_type") == "Group" && recordString(group, "client_id") == clientID && recordString(group, "id") == tuple.GetResource().GetId() {
						relevant = true
						break
					}
				}
			}
			if relevant {
				mark(clientID, coreID)
			}
		}
		return nil
	}
	if subjectSet := tuple.GetTarget().GetSubjectSet(); subjectSet == nil || subjectSet.GetResource() == nil || subjectSet.GetResource().GetType() != "group" {
		return nil
	}
	coreIDs, err := s.affectedGroupMemberUsers(ctx, "", tuple)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if recordString(row, "resource_type") != "Group" || recordString(row, "id") != tuple.GetResource().GetId() {
			continue
		}
		for _, coreID := range coreIDs {
			mark(recordString(row, "client_id"), coreID)
		}
	}
	return nil
}

func tupleKey(t *proto.RelationshipTuple) string {
	return t.GetRelation() + "\x00" + t.GetResource().GetType() + "\x00" + t.GetResource().GetId() + "\x00" + t.GetTarget().String()
}
func (s *CompactService) desiredUser(r storedResource, active bool) []*proto.RelationshipTuple {
	if !active || r.CoreUserID == "" {
		return nil
	}
	c := s.clients[r.ClientID]
	if c == nil {
		return nil
	}
	out := make([]*proto.RelationshipTuple, 0, len(c.projections))
	for _, p := range c.projections {
		out = append(out, userTuple(r.CoreUserID, p))
	}
	return out
}
func (s *CompactService) Create(ctx context.Context, clientID string, input userInput) (*User, error) {
	if err := validateResourceSchemas(input.Schemas, UserSchemaURN); err != nil {
		return nil, err
	}
	if input.UserName == nil || strings.TrimSpace(*input.UserName) == "" {
		return nil, invalid("userName is required")
	}
	u := persistedUser{UserName: *input.UserName}
	if input.ExternalID != nil {
		u.ExternalID = *input.ExternalID
	}
	if input.Active != nil {
		u.Active = *input.Active
	}
	if input.DisplayName != nil {
		u.DisplayName = *input.DisplayName
		u.DisplayNameSet = true
	}
	if input.Name != nil {
		u.Name = *input.Name
	}
	if input.Emails != nil {
		u.Emails = *input.Emails
		if err := validateEmails(u.Emails); err != nil {
			return nil, err
		}
	}
	email, e := s.emailFor(u)
	if e != nil {
		return nil, e
	}
	id := uuid.NewString()
	now := s.nowUTC()
	// The transaction serializes the compact metadata snapshot across
	// replicas. Provider-derived active state is outside this transaction and
	// is subject to the provider-atomicity limitation documented for SCIM.
	tx, e := s.db.Transaction(ctx, []string{coredata.StoreUsers, coredata.StoreSCIMResources}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if e != nil {
		return nil, unavailable("could not start SCIM transaction")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(ctx)
		}
	}()
	var displayName *string
	if u.DisplayNameSet {
		displayName = &u.DisplayName
	}
	existing, e := tx.ObjectStore(coredata.StoreUsers).Index("by_normalized_email").GetAll(ctx, email)
	if e != nil {
		return nil, unavailable("could not inspect linked user")
	}
	if len(existing) > 1 {
		return nil, conflict("email is linked to multiple core users")
	}
	if len(existing) == 1 {
		existingID := recordString(existing[0], "id")
		shared, sharedErr := coreUserReferencedByOtherClientStore(ctx, tx.ObjectStore(coredata.StoreSCIMResources), existingID, clientID, "")
		if sharedErr != nil {
			return nil, unavailable("could not inspect linked SCIM users")
		}
		if shared && u.DisplayNameSet {
			currentCore, coreErr := coredata.GetUserInTransaction(ctx, tx, existingID)
			if coreErr != nil {
				return nil, unavailable("could not load linked user")
			}
			if currentCore.DisplayName != u.DisplayName {
				return nil, conflict("linked core user is referenced by another SCIM client")
			}
		}
	}
	cu, e := coredata.LinkUserInTransaction(ctx, tx, "", email, displayName, now)
	if e != nil {
		_ = tx.Abort(ctx)
		if errors.Is(e, coredata.ErrUserEmailConflict) {
			return nil, conflict(e.Error())
		}
		return nil, unavailable("could not link core user")
	}
	row := storedResource{ID: id, ClientID: clientID, ResourceType: "User", ExternalID: u.ExternalID, CoreUserID: cu.ID, UserName: normalize(u.UserName), Profile: userProfile{Name: u.Name, Emails: u.Emails}, CreatedAt: now, UpdatedAt: now}
	record, e := resourceRecord(row)
	if e != nil {
		return nil, unavailable("could not encode SCIM user")
	}
	if e = tx.ObjectStore(coredata.StoreSCIMResources).Add(ctx, record); e != nil {
		_ = tx.Abort(ctx)
		if errors.Is(e, idb.ErrAlreadyExists) {
			return nil, conflict("SCIM user already exists")
		}
		return nil, unavailable("could not persist SCIM user")
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, unavailable("could not commit SCIM user")
	}
	committed = true
	if e = s.apply(ctx, nil, s.desiredUser(row, u.Active)); e != nil {
		return nil, unavailable("could not apply authorization projection")
	}
	reloaded, e := s.findUser(ctx, clientID, id)
	if e != nil {
		return nil, unavailable("could not reload SCIM user")
	}
	v, e := s.userValue(ctx, reloaded)
	if e != nil {
		return nil, e
	}
	return &v, nil
}
func (s *CompactService) Get(ctx context.Context, clientID, id string) (*User, error) {
	r, e := s.findUser(ctx, clientID, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return nil, notFound()
		}
		return nil, unavailable("could not load SCIM user")
	}
	u, e := s.userValue(ctx, r)
	if e != nil {
		return nil, e
	}
	return &u, nil
}
func (s *CompactService) list(ctx context.Context, clientID, raw string, start, count int) (listResponse[User], error) {
	clauses, e := parseFilter(raw)
	if e != nil {
		return listResponse[User]{}, invalidFilter(e.Error())
	}
	rs, e := s.listRecords(ctx, clientID, "User")
	if e != nil {
		return listResponse[User]{}, unavailable("could not list SCIM users")
	}
	sort.Slice(rs, func(i, j int) bool { return recordString(rs[i], "id") < recordString(rs[j], "id") })
	// Filtering only needs the compact, stored attributes. Keep the matching
	// rows in their stored form and slice the page before userValue performs
	// provider/core-user hydration (which also walks nested groups).
	matching := make([]idb.Record, 0, len(rs))
	for _, rr := range rs {
		r := stored(rr)
		if !matchesFilter(persistedUser{ExternalID: r.ExternalID, UserName: r.UserName, Emails: r.Profile.Emails}, clauses) {
			continue
		}
		matching = append(matching, rr)
	}
	if start > len(matching)+1 {
		start = len(matching) + 1
	}
	end := pageMin(start-1+count, len(matching))
	page := matching[start-1 : end]
	out := make([]User, 0, len(page))
	for i := range page {
		u, e := s.userValue(ctx, stored(page[i]))
		if e != nil {
			return listResponse[User]{}, e
		}
		out = append(out, u)
	}
	return listResponse[User]{Schemas: []string{ListSchemaURN}, TotalResults: len(matching), StartIndex: start, ItemsPerPage: len(out), Resources: out}, nil
}
func pageMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func (s *CompactService) mutateUser(ctx context.Context, clientID, id, ifMatch string, u persistedUser) (*User, error) {
	unlock := s.lock(id)
	defer unlock()
	r, e := s.findUser(ctx, clientID, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return nil, notFound()
		}
		return nil, unavailable("could not load SCIM user")
	}
	old, e := s.userValue(ctx, r)
	if e != nil {
		return nil, e
	}
	actual, e := s.actualUserTuples(ctx, r)
	if e != nil {
		return nil, unavailable("could not read authorization state")
	}
	email, e := s.emailFor(u)
	if e != nil {
		return nil, e
	}
	now := s.nowUTC()
	tx, e := s.db.Transaction(ctx, []string{coredata.StoreUsers, coredata.StoreSCIMResources}, idb.TransactionReadwrite, idb.TransactionOptions{})
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
			return nil, notFound()
		}
		return nil, unavailable("could not load SCIM user")
	}
	if recordString(txRow, "client_id") != clientID || recordString(txRow, "resource_type") != "User" {
		return nil, notFound()
	}
	currentCore, e := coredata.GetUserInTransaction(ctx, tx, recordString(txRow, "core_user_id"))
	if e != nil {
		return nil, unavailable("could not load linked user")
	}
	desiredDisplayName := currentCore.DisplayName
	if u.DisplayNameSet {
		desiredDisplayName = u.DisplayName
	}
	shared, e := coreUserReferencedByOtherClientStore(ctx, tx.ObjectStore(coredata.StoreSCIMResources), currentCore.ID, clientID, id)
	if e != nil {
		return nil, unavailable("could not inspect linked SCIM users")
	}
	if shared && (normalize(currentCore.Email) != email || currentCore.DisplayName != desiredDisplayName) {
		return nil, conflict("linked core user is referenced by another SCIM client")
	}
	if ifMatch != "" && ifMatch != "*" && ifMatch != old.Meta.Version {
		return nil, &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	snapshotRecord, e := resourceRecord(r)
	if e != nil {
		return nil, unavailable("could not encode SCIM user")
	}
	if !sameResourceContent(txRow, snapshotRecord) {
		if ifMatch == "" {
			return nil, unavailable("SCIM resource changed concurrently; retry the request")
		}
		return nil, &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	r = stored(txRow)
	if u.Active == old.Active && u.ExternalID == r.ExternalID && normalize(u.UserName) == r.UserName && desiredDisplayName == old.DisplayName && currentCore.DisplayName == old.DisplayName && reflect.DeepEqual(u.Name, r.Profile.Name) && reflect.DeepEqual(u.Emails, r.Profile.Emails) && sameTuples(actual, s.desiredUser(r, u.Active)) {
		return &old, nil
	}
	var displayName *string
	if u.DisplayNameSet {
		displayName = &u.DisplayName
	}
	cu, e := coredata.LinkUserInTransaction(ctx, tx, r.CoreUserID, email, displayName, now)
	if e != nil {
		if errors.Is(e, coredata.ErrUserEmailConflict) {
			return nil, conflict(e.Error())
		}
		return nil, unavailable("could not link core user")
	}
	r.CoreUserID = cu.ID
	r.ExternalID = u.ExternalID
	r.UserName = normalize(u.UserName)
	r.Profile = userProfile{Name: u.Name, Emails: u.Emails}
	r.UpdatedAt = now
	record, e := resourceRecord(r)
	if e != nil {
		return nil, unavailable("could not encode SCIM user")
	}
	if e = tx.ObjectStore(coredata.StoreSCIMResources).Put(ctx, record); e != nil {
		if errors.Is(e, idb.ErrAlreadyExists) {
			return nil, conflict("userName must be unique")
		}
		return nil, unavailable("could not persist SCIM user")
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, unavailable("could not commit SCIM user")
	}
	committed = true
	if e = s.apply(ctx, actual, s.desiredUser(r, u.Active)); e != nil {
		return nil, unavailable("could not apply authorization projection")
	}
	reloaded, e := s.findUser(ctx, clientID, id)
	if e != nil {
		return nil, unavailable("could not reload SCIM user")
	}
	v, e := s.userValue(ctx, reloaded)
	if e != nil {
		return nil, e
	}
	return &v, nil
}

func (s *CompactService) Replace(ctx context.Context, clientID, id, ifMatch string, in userInput) (*User, error) {
	if err := validateResourceSchemas(in.Schemas, UserSchemaURN); err != nil {
		return nil, err
	}
	if in.UserName == nil || strings.TrimSpace(*in.UserName) == "" {
		return nil, invalid("userName is required")
	}
	u := persistedUser{UserName: *in.UserName, DisplayNameSet: true}
	if in.ExternalID != nil {
		u.ExternalID = *in.ExternalID
	}
	if in.Active != nil {
		u.Active = *in.Active
	}
	if in.DisplayName != nil {
		u.DisplayName = *in.DisplayName
		u.DisplayNameSet = true
	}
	if in.Name != nil {
		u.Name = *in.Name
	}
	if in.Emails != nil {
		u.Emails = *in.Emails
		if err := validateEmails(u.Emails); err != nil {
			return nil, err
		}
	}
	return s.mutateUser(ctx, clientID, id, ifMatch, u)
}
func (s *CompactService) Delete(ctx context.Context, clientID, id, ifMatch string) error {
	unlock := s.lock(id)
	defer unlock()
	r, e := s.findUser(ctx, clientID, id)
	if e != nil {
		if errors.Is(e, idb.ErrNotFound) {
			return notFound()
		}
		return unavailable("could not load SCIM user")
	}
	u, e := s.userValue(ctx, r)
	if e != nil {
		return e
	}
	actual, e := s.actualUserTuples(ctx, r)
	if e != nil {
		return unavailable("could not read authorization state")
	}
	if ifMatch != "" && ifMatch != "*" && ifMatch != u.Meta.Version {
		return &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	// Revalidate the metadata snapshot under a write transaction immediately
	// before provider cleanup. The provider mutation is outside this transaction;
	// cross-replica conditional atomicity remains part of the provider limitation.
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
			return notFound()
		}
		return unavailable("could not revalidate SCIM user")
	}
	snapshotRecord, e := resourceRecord(r)
	if e != nil {
		return unavailable("could not encode SCIM user")
	}
	if !sameResourceContent(current, snapshotRecord) {
		if ifMatch == "" {
			return unavailable("SCIM resource changed concurrently; retry the request")
		}
		return &Error{Status: 412, Detail: "SCIM resource version does not match"}
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable("could not commit SCIM delete transaction")
	}
	committed = true
	if e = s.apply(ctx, actual, nil); e != nil {
		return unavailable("could not remove authorization relationships")
	}
	if e = s.removeUserFromClientGroups(ctx, clientID, r.CoreUserID); e != nil {
		return unavailable("could not remove authorization relationships")
	}
	if e = s.db.ObjectStore(coredata.StoreSCIMResources).Delete(ctx, id); e != nil {
		return unavailable("could not delete SCIM user")
	}
	return nil
}
func (s *CompactService) actualUserTuples(ctx context.Context, r storedResource) ([]*proto.RelationshipTuple, error) {
	desired := s.desiredUser(r, true)
	actual := make([]*proto.RelationshipTuple, 0, len(desired))
	for _, tuple := range desired {
		present, err := s.hasTuple(ctx, tuple)
		if err != nil {
			return nil, err
		}
		if present {
			actual = append(actual, tuple)
		}
	}
	return actual, nil
}

// removeUserFromClientGroups cleans references only from SCIM groups in the
// same client namespace. Runtime relationships for unrelated authorization
// resources are left untouched.
func (s *CompactService) removeUserFromClientGroups(ctx context.Context, clientID, coreID string) error {
	groups, err := s.listRecords(ctx, clientID, "Group")
	if err != nil {
		return err
	}
	for _, row := range groups {
		groupID := recordString(row, "id")
		relationships, err := s.listRelationships(ctx, &proto.RelationshipFilter{Resource: &proto.Resource{Type: "group", Id: groupID}, Relation: "member", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME}, 100)
		if err != nil {
			return err
		}
		for _, relationship := range relationships {
			tuple := relationship.GetTuple()
			subject := tuple.GetTarget().GetSubject()
			if subject == nil || subject.GetId() != "user:"+coreID {
				continue
			}
			if err := s.deleteRelationship(ctx, tuple); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *CompactService) IsEligible(ctx context.Context, coreID, email string) (bool, error) {
	owner := s.domainOwners[domain(email)]
	if owner == "" {
		return true, nil
	}
	rs, e := s.db.ObjectStore(coredata.StoreSCIMResources).Index("by_client_core_user").GetAll(ctx, []any{owner, "User", coreID})
	if e != nil {
		return false, e
	}
	if len(rs) != 1 {
		return false, nil
	}
	return s.userActive(ctx, stored(rs[0]))
}
func domain(email string) string {
	p := strings.LastIndex(normalize(email), "@")
	if p < 0 {
		return ""
	}
	return normalize(email[p+1:])
}

func (s *CompactService) userGroupsNested(ctx context.Context, cid, coreID string) ([]GroupRef, error) {
	rows, e := s.listRecords(ctx, cid, "Group")
	if e != nil {
		return nil, e
	}
	type node struct {
		members []Member
	}
	nodes := map[string]node{}
	for _, rr := range rows {
		g := stored(rr)
		m, e := s.liveMembers(ctx, cid, g.ID)
		if e != nil {
			return nil, e
		}
		nodes[g.ID] = node{members: m}
	}
	refs := []GroupRef{}
	for gid := range nodes {
		queue := []struct {
			id       string
			indirect bool
		}{{gid, false}}
		seen := map[string]bool{}
		for len(queue) > 0 {
			q := queue[0]
			queue = queue[1:]
			if seen[q.id] {
				continue
			}
			seen[q.id] = true
			cur, ok := nodes[q.id]
			if !ok {
				continue
			}
			for _, m := range cur.members {
				switch m.Type {
				case "User":
					u, e := s.findUser(ctx, cid, m.Value)
					if e != nil && !errors.Is(e, idb.ErrNotFound) {
						return nil, e
					}
					if e == nil && u.CoreUserID == coreID {
						typ := "direct"
						if q.indirect {
							typ = "indirect"
						}
						refs = append(refs, GroupRef{Value: gid, Ref: s.baseURL + "/scim/v2/Groups/" + gid, Type: typ})
					}
				case "Group":
					queue = append(queue, struct {
						id       string
						indirect bool
					}{m.Value, true})
				}
			}
		}
	}
	uniq := map[string]GroupRef{}
	for _, r := range refs {
		if old, ok := uniq[r.Value]; !ok || old.Type == "indirect" {
			uniq[r.Value] = r
		}
	}
	refs = refs[:0]
	for _, r := range uniq {
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Value < refs[j].Value })
	return refs, nil
}

// Group operations live in compact_groups.go.
