package agentroute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const storeAgentRoutes = "agent_routes"

var agentRoutesSchema = idb.ObjectStoreOptions{
	Indexes: []idb.IndexSchema{
		{Name: "by_owner", KeyPath: []string{"owner_subject_id"}},
		{Name: "by_owner_state", KeyPath: []string{"owner_subject_id", "state"}},
		{Name: "by_provider", KeyPath: []string{"provider_name"}},
		{Name: "by_idempotency", KeyPath: []string{"idempotency_digest"}, Unique: true},
	},
	Columns: []idb.ColumnDef{
		{Name: "id", Type: idb.TypeString, PrimaryKey: true},
		{Name: "owner_subject_id", Type: idb.TypeString, NotNull: true},
		{Name: "credential_subject_id", Type: idb.TypeString},
		{Name: "provider_name", Type: idb.TypeString, NotNull: true},
		{Name: "config_revision", Type: idb.TypeString, NotNull: true},
		{Name: "authority_ref", Type: idb.TypeString},
		{Name: "state", Type: idb.TypeString, NotNull: true},
		{Name: "idempotency_digest", Type: idb.TypeString, NotNull: true, Unique: true},
		{Name: "request_fingerprint", Type: idb.TypeString},
		{Name: "created_at", Type: idb.TypeTime, NotNull: true},
		{Name: "updated_at", Type: idb.TypeTime, NotNull: true},
	},
}

type IndexedDBStore struct {
	db    coreindexeddb.IndexedDB
	store idb.ObjectStore
	now   func() time.Time
}

func NewIndexedDBStore(ctx context.Context, db coreindexeddb.IndexedDB) (*IndexedDBStore, error) {
	if db == nil {
		return nil, fmt.Errorf("agent route store requires indexeddb")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := db.CreateObjectStore(ctx, storeAgentRoutes, agentRoutesSchema); err != nil {
		return nil, fmt.Errorf("create agent routes store: %w", err)
	}
	return &IndexedDBStore{
		db:    db,
		store: db.ObjectStore(storeAgentRoutes),
		now:   time.Now,
	}, nil
}

func (s *IndexedDBStore) Create(ctx context.Context, input CreateRequest) (*Route, bool, error) {
	req, err := normalizeCreateRequest(input)
	if err != nil {
		return nil, false, err
	}
	if req.IdempotencyKey != "" {
		existing, err := s.findByIdempotency(ctx, req.OwnerSubjectID, req.IdempotencyKey)
		switch {
		case err == nil:
			if routeFingerprint(existing) != req.RequestFingerprint {
				return nil, false, fmt.Errorf("%w: idempotency key was used with a different request", ErrConflict)
			}
			return recordToRoute(existing), false, nil
		case errors.Is(err, idb.ErrNotFound):
		default:
			return nil, false, fmt.Errorf("find agent route by idempotency: %w", err)
		}
	}

	now := s.now().UTC()
	if req.CreatedAt.IsZero() {
		req.CreatedAt = now
	}
	if req.UpdatedAt.IsZero() {
		req.UpdatedAt = req.CreatedAt
	}
	rec := createRequestToRecord(req)
	if err := s.store.Add(ctx, rec); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			existing, lookupErr := s.lookupCreateConflict(ctx, req)
			if lookupErr == nil {
				if !sameCreate(existing, req) {
					return nil, false, fmt.Errorf("%w: route id or idempotency key was used with a different request", ErrConflict)
				}
				return recordToRoute(existing), false, nil
			}
		}
		return nil, false, fmt.Errorf("create agent route: %w", err)
	}
	return recordToRoute(rec), true, nil
}

func (s *IndexedDBStore) GetOwned(ctx context.Context, agentID, ownerSubjectID string) (*Route, error) {
	agentID = strings.TrimSpace(agentID)
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	if agentID == "" || ownerSubjectID == "" {
		return nil, ErrNotFound
	}
	rec, err := s.store.Get(ctx, agentID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get agent route: %w", err)
	}
	if recordString(rec, "owner_subject_id") != ownerSubjectID {
		return nil, ErrNotFound
	}
	return recordToRoute(rec), nil
}

func (s *IndexedDBStore) FindByIdempotency(ctx context.Context, ownerSubjectID, idempotencyKey string) (*Route, error) {
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if ownerSubjectID == "" || idempotencyKey == "" {
		return nil, ErrNotFound
	}
	rec, err := s.findByIdempotency(ctx, ownerSubjectID, idempotencyKey)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find agent route by idempotency: %w", err)
	}
	return recordToRoute(rec), nil
}

func (s *IndexedDBStore) ListOwned(ctx context.Context, ownerSubjectID string, state State) ([]*Route, error) {
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	if ownerSubjectID == "" {
		return nil, fmt.Errorf("list agent routes requires owner subject id")
	}
	var (
		records []idb.Record
		err     error
	)
	switch {
	case state == "":
		records, err = s.store.Index("by_owner").GetAll(ctx, ownerSubjectID)
	case validState(state):
		records, err = s.store.Index("by_owner_state").GetAll(ctx, []any{ownerSubjectID, string(state)})
	default:
		return nil, fmt.Errorf("list agent routes: invalid state %q", state)
	}
	if err != nil {
		return nil, fmt.Errorf("list agent routes: %w", err)
	}
	routes := make([]*Route, 0, len(records))
	for _, rec := range records {
		routes = append(routes, recordToRoute(rec))
	}
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].UpdatedAt.Equal(routes[j].UpdatedAt) {
			return routes[i].AgentID < routes[j].AgentID
		}
		return routes[i].UpdatedAt.After(routes[j].UpdatedAt)
	})
	return routes, nil
}

func (s *IndexedDBStore) CompareAndSwapRevision(
	ctx context.Context,
	agentID string,
	ownerSubjectID string,
	expectedRevision string,
	nextRevision string,
) (*Route, error) {
	agentID = strings.TrimSpace(agentID)
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	expectedRevision = strings.TrimSpace(expectedRevision)
	nextRevision = strings.TrimSpace(nextRevision)
	if agentID == "" || ownerSubjectID == "" || expectedRevision == "" || nextRevision == "" {
		return nil, fmt.Errorf("compare-and-swap agent route revision requires agent, owner, expected, and next revisions")
	}
	return s.mutate(ctx, agentID, ownerSubjectID, func(route *Route) error {
		if route.State != StateActive {
			return fmt.Errorf("%w: archived agent configuration cannot be updated", ErrConflict)
		}
		if route.ConfigRevision == nextRevision {
			return nil
		}
		if route.ConfigRevision != expectedRevision {
			return fmt.Errorf(
				"%w: config revision is %q, expected %q",
				ErrConflict,
				route.ConfigRevision,
				expectedRevision,
			)
		}
		route.ConfigRevision = nextRevision
		return nil
	})
}

func (s *IndexedDBStore) Archive(ctx context.Context, agentID, ownerSubjectID string) (*Route, error) {
	return s.mutate(ctx, strings.TrimSpace(agentID), strings.TrimSpace(ownerSubjectID), func(route *Route) error {
		route.State = StateArchived
		return nil
	})
}

func (s *IndexedDBStore) mutate(
	ctx context.Context,
	agentID string,
	ownerSubjectID string,
	update func(*Route) error,
) (*Route, error) {
	if agentID == "" || ownerSubjectID == "" {
		return nil, ErrNotFound
	}
	tx, err := s.db.Transaction(
		ctx,
		[]string{storeAgentRoutes},
		idb.TransactionReadwrite,
		idb.TransactionOptions{DurabilityHint: idb.TransactionDurabilityStrict},
	)
	if err != nil {
		return nil, fmt.Errorf("start agent route transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	store := tx.ObjectStore(storeAgentRoutes)
	rec, err := store.Get(ctx, agentID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get agent route for update: %w", err)
	}
	route := recordToRoute(rec)
	if route.OwnerSubjectID != ownerSubjectID {
		return nil, ErrNotFound
	}
	if err := update(route); err != nil {
		return nil, err
	}
	route.UpdatedAt = s.now().UTC()
	updated := routeToRecord(route, recordString(rec, "idempotency_digest"), routeFingerprint(rec))
	if err := store.Put(ctx, updated); err != nil {
		return nil, fmt.Errorf("update agent route: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit agent route update: %w", err)
	}
	committed = true
	return cloneRoute(route), nil
}

func (s *IndexedDBStore) findByIdempotency(
	ctx context.Context,
	ownerSubjectID string,
	idempotencyKey string,
) (idb.Record, error) {
	return s.store.Index("by_idempotency").Get(ctx, idempotencyDigest(ownerSubjectID, idempotencyKey))
}

func (s *IndexedDBStore) lookupCreateConflict(ctx context.Context, req CreateRequest) (idb.Record, error) {
	if req.IdempotencyKey != "" {
		if rec, err := s.findByIdempotency(ctx, req.OwnerSubjectID, req.IdempotencyKey); err == nil {
			return rec, nil
		}
	}
	return s.store.Get(ctx, req.AgentID)
}

func createRequestToRecord(req CreateRequest) idb.Record {
	digest := "agent:" + req.AgentID
	if req.IdempotencyKey != "" {
		digest = idempotencyDigest(req.OwnerSubjectID, req.IdempotencyKey)
	}
	return routeToRecord(&req.Route, digest, req.RequestFingerprint)
}

func routeToRecord(route *Route, idempotencyDigestValue, requestFingerprint string) idb.Record {
	return idb.Record{
		"id":                    route.AgentID,
		"owner_subject_id":      route.OwnerSubjectID,
		"credential_subject_id": route.CredentialSubjectID,
		"provider_name":         route.ProviderName,
		"config_revision":       route.ConfigRevision,
		"authority_ref":         route.AuthorityRef,
		"state":                 string(route.State),
		"idempotency_digest":    idempotencyDigestValue,
		"request_fingerprint":   requestFingerprint,
		"created_at":            route.CreatedAt,
		"updated_at":            route.UpdatedAt,
	}
}

func recordToRoute(rec idb.Record) *Route {
	return &Route{
		AgentID:             recordString(rec, "id"),
		OwnerSubjectID:      recordString(rec, "owner_subject_id"),
		CredentialSubjectID: recordString(rec, "credential_subject_id"),
		ProviderName:        recordString(rec, "provider_name"),
		ConfigRevision:      recordString(rec, "config_revision"),
		AuthorityRef:        recordString(rec, "authority_ref"),
		RequestFingerprint:  routeFingerprint(rec),
		State:               State(recordString(rec, "state")),
		CreatedAt:           recordTime(rec, "created_at"),
		UpdatedAt:           recordTime(rec, "updated_at"),
	}
}

func sameCreate(rec idb.Record, req CreateRequest) bool {
	route := recordToRoute(rec)
	return route.AgentID == req.AgentID &&
		route.OwnerSubjectID == req.OwnerSubjectID &&
		route.CredentialSubjectID == req.CredentialSubjectID &&
		route.ProviderName == req.ProviderName &&
		route.ConfigRevision == req.ConfigRevision &&
		route.AuthorityRef == req.AuthorityRef &&
		route.State == req.State &&
		route.RequestFingerprint == req.RequestFingerprint
}

func idempotencyDigest(ownerSubjectID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ownerSubjectID) + "\x00" + strings.TrimSpace(idempotencyKey)))
	return hex.EncodeToString(sum[:])
}

func routeFingerprint(rec idb.Record) string {
	return recordString(rec, "request_fingerprint")
}

func recordString(rec idb.Record, key string) string {
	switch value := rec[key].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return ""
	}
}

func recordTime(rec idb.Record, key string) time.Time {
	switch value := rec[key].(type) {
	case time.Time:
		return value
	case *time.Time:
		if value != nil {
			return *value
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

var _ Store = (*IndexedDBStore)(nil)
