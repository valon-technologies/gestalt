package coredata

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const physicalDevStorePrefix = "gd_"

func physicalDevelopmentStoreName(namespaceID, logicalName string) string {
	h := sha256.New()
	_, _ = h.Write([]byte("namespace\x00" + namespaceID))
	namespaceHash := base32.StdEncoding.EncodeToString(h.Sum(nil))[:20]
	h.Reset()
	_, _ = h.Write([]byte("store\x00" + logicalName))
	storeHash := base32.StdEncoding.EncodeToString(h.Sum(nil))[:20]
	return physicalDevStorePrefix + namespaceHash + "_" + storeHash
}

type RemoteIndexedDBNamespaceState string

const (
	NamespacePreparing      RemoteIndexedDBNamespaceState = "preparing"
	NamespaceActive         RemoteIndexedDBNamespaceState = "active"
	NamespaceCleanupPending RemoteIndexedDBNamespaceState = "cleanup_pending"
	NamespaceCleaned        RemoteIndexedDBNamespaceState = "cleaned"
)

type RemoteIndexedDBNamespace struct {
	ID                    string
	RegistrationID        string
	Generation            uint64
	SessionID             string
	OwnerSubjectID        string
	AppName               string
	ProviderName          string
	DatabaseName          string
	State                 RemoteIndexedDBNamespaceState
	LeaseExpiresAt        time.Time
	CleanupAfter          time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CleanupHolder         string
	CleanupLeaseExpiresAt time.Time
	CleanupAttemptCount   int
	LastCleanupError      string
}

type RemoteIndexedDBNamespaceStore struct {
	ID           string
	NamespaceID  string
	LogicalName  string
	PhysicalName string
	CreatedAt    time.Time
	DeletedAt    *time.Time
}

type AppIndexedDBBinding struct {
	ProviderName  string
	DatabaseName  string
	AllowedStores []string
}

type RemoteIndexedDBNamespaceService struct {
	db  indexeddb.IndexedDB
	now func() time.Time
}

func NewRemoteIndexedDBNamespaceService(ds indexeddb.IndexedDB) *RemoteIndexedDBNamespaceService {
	return &RemoteIndexedDBNamespaceService{db: ds, now: time.Now}
}

func (s *RemoteIndexedDBNamespaceService) SetClock(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

func (s *RemoteIndexedDBNamespaceService) nowTime() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC().Truncate(time.Millisecond)
	}
	return normalizedRemoteTime(s.now())
}

func (s *RemoteIndexedDBNamespaceService) Prepare(
	ctx context.Context,
	registrationID string,
	generation uint64,
	sessionID string,
	ownerSubjectID string,
	appName string,
	binding *AppIndexedDBBinding,
	leaseExpiresAt time.Time,
) (*RemoteIndexedDBNamespace, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: registration_id is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: session_id is required")
	}
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	if ownerSubjectID == "" {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: owner_subject_id is required")
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: app_name is required")
	}
	if binding == nil {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: binding is required")
	}
	providerName := strings.TrimSpace(binding.ProviderName)
	if providerName == "" {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: provider is required")
	}
	databaseName := strings.TrimSpace(binding.DatabaseName)
	if databaseName == "" {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: database is required")
	}
	leaseExpiresAt = normalizedRemoteTime(leaseExpiresAt)
	if leaseExpiresAt.IsZero() {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: lease_expires_at is required")
	}

	now := s.nowTime()
	namespace := &RemoteIndexedDBNamespace{
		ID:             uuid.NewString(),
		RegistrationID: registrationID,
		Generation:     generation,
		SessionID:      sessionID,
		OwnerSubjectID: ownerSubjectID,
		AppName:        appName,
		ProviderName:   providerName,
		DatabaseName:   databaseName,
		State:          NamespacePreparing,
		LeaseExpiresAt: leaseExpiresAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	tx, err := s.db.Transaction(ctx, []string{StoreRemoteIndexedDBNamespaces, StoreRemoteIndexedDBNamespaceStores}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	existingRec, err := tx.ObjectStore(StoreRemoteIndexedDBNamespaces).Index("by_session_app").Get(ctx, []any{sessionID, appName})
	if err != nil && !errors.Is(err, idb.ErrNotFound) {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: check existing: %w", err)
	}
	if existingRec != nil {
		existing := recordToRemoteIndexedDBNamespace(existingRec)
		if existing.RegistrationID != registrationID || existing.Generation != generation {
			return nil, fmt.Errorf("prepare remote indexeddb namespace: session/app already bound to another registration")
		}
		// Idempotent re-preparation: update the prepared namespace in place.
		namespace.ID = existing.ID
		namespace.CreatedAt = existing.CreatedAt
		namespace.CleanupHolder = existing.CleanupHolder
		namespace.CleanupLeaseExpiresAt = existing.CleanupLeaseExpiresAt
		namespace.CleanupAttemptCount = existing.CleanupAttemptCount
		namespace.LastCleanupError = existing.LastCleanupError
	}

	if err := tx.ObjectStore(StoreRemoteIndexedDBNamespaces).Put(ctx, remoteIndexedDBNamespaceRecord(namespace)); err != nil {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: store namespace: %w", err)
	}

	for _, logicalName := range binding.AllowedStores {
		logicalName = strings.TrimSpace(logicalName)
		if logicalName == "" {
			continue
		}
		physicalName := physicalDevelopmentStoreName(namespace.ID, logicalName)
		storeRec := idb.Record{
			"id":            uuid.NewString(),
			"namespace_id":  namespace.ID,
			"logical_name":  logicalName,
			"physical_name": physicalName,
			"created_at":    now,
			"deleted_at":    nil,
		}
		if err := tx.ObjectStore(StoreRemoteIndexedDBNamespaceStores).Put(ctx, storeRec); err != nil {
			return nil, fmt.Errorf("prepare remote indexeddb namespace: track allowed store %q: %w", logicalName, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("prepare remote indexeddb namespace: commit: %w", err)
	}
	committed = true
	return namespace, nil
}

func (s *RemoteIndexedDBNamespaceService) ResolveActive(
	ctx context.Context,
	namespaceID string,
	registrationID string,
	sessionID string,
	appName string,
	generation uint64,
) (*RemoteIndexedDBNamespace, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("resolve remote indexeddb namespace: service is not configured")
	}
	namespaceID = strings.TrimSpace(namespaceID)
	if namespaceID == "" {
		return nil, fmt.Errorf("resolve remote indexeddb namespace: namespace_id is required")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return nil, fmt.Errorf("resolve remote indexeddb namespace: registration_id is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("resolve remote indexeddb namespace: session_id is required")
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("resolve remote indexeddb namespace: app_name is required")
	}

	rec, err := s.db.ObjectStore(StoreRemoteIndexedDBNamespaces).Get(ctx, namespaceID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, ErrNotRegistered
	}
	if err != nil {
		return nil, fmt.Errorf("resolve remote indexeddb namespace: load: %w", err)
	}
	ns := recordToRemoteIndexedDBNamespace(rec)
	now := s.nowTime()
	if ns.RegistrationID != registrationID || ns.SessionID != sessionID || ns.AppName != appName || ns.Generation != generation {
		return nil, ErrNotRegistered
	}
	if ns.State != NamespaceActive && ns.State != NamespacePreparing {
		return nil, ErrNotRegistered
	}
	if !ns.LeaseExpiresAt.After(now) {
		return nil, ErrNotRegistered
	}
	return ns, nil
}

func (s *RemoteIndexedDBNamespaceService) ResolvePhysicalName(ctx context.Context, namespaceID, logicalName string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("resolve physical store name: service is not configured")
	}
	namespaceID = strings.TrimSpace(namespaceID)
	if namespaceID == "" {
		return "", fmt.Errorf("resolve physical store name: namespace_id is required")
	}
	logicalName = strings.TrimSpace(logicalName)
	if logicalName == "" {
		return "", fmt.Errorf("resolve physical store name: logical_name is required")
	}

	rec, err := s.db.ObjectStore(StoreRemoteIndexedDBNamespaceStores).Index("by_namespace_logical").Get(ctx, []any{namespaceID, logicalName})
	if errors.Is(err, idb.ErrNotFound) {
		// No tracked mapping yet; compute it deterministically.
		return physicalDevelopmentStoreName(namespaceID, logicalName), nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve physical store name: load mapping: %w", err)
	}
	store := recordToRemoteIndexedDBNamespaceStore(rec)
	if store.DeletedAt != nil {
		return "", idb.ErrNotFound
	}
	return store.PhysicalName, nil
}

func (s *RemoteIndexedDBNamespaceService) TrackStore(
	ctx context.Context,
	namespaceID string,
	logicalName string,
	physicalName string,
) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("track remote indexeddb namespace store: service is not configured")
	}
	namespaceID = strings.TrimSpace(namespaceID)
	if namespaceID == "" {
		return fmt.Errorf("track remote indexeddb namespace store: namespace_id is required")
	}
	logicalName = strings.TrimSpace(logicalName)
	if logicalName == "" {
		return fmt.Errorf("track remote indexeddb namespace store: logical_name is required")
	}
	physicalName = strings.TrimSpace(physicalName)
	if physicalName == "" {
		return fmt.Errorf("track remote indexeddb namespace store: physical_name is required")
	}

	store := s.db.ObjectStore(StoreRemoteIndexedDBNamespaceStores)
	now := s.nowTime()

	existingRec, err := store.Index("by_namespace_logical").Get(ctx, []any{namespaceID, logicalName})
	if err != nil && !errors.Is(err, idb.ErrNotFound) {
		return fmt.Errorf("track remote indexeddb namespace store: check existing: %w", err)
	}
	if existingRec != nil {
		existing := recordToRemoteIndexedDBNamespaceStore(existingRec)
		if existing.PhysicalName != physicalName {
			return fmt.Errorf("track remote indexeddb namespace store: logical name %q already mapped to a different physical name", logicalName)
		}
		if existing.DeletedAt != nil {
			existing.DeletedAt = nil
			existing.CreatedAt = now
			return store.Put(ctx, remoteIndexedDBNamespaceStoreRecord(existing))
		}
		return nil
	}

	// Check that no other logical name in this namespace already uses this physical name.
	collisionRec, err := store.Index("by_namespace").Get(ctx, []any{namespaceID, physicalName})
	if err != nil && !errors.Is(err, idb.ErrNotFound) {
		return fmt.Errorf("track remote indexeddb namespace store: check collision: %w", err)
	}
	if collisionRec != nil {
		collision := recordToRemoteIndexedDBNamespaceStore(collisionRec)
		if collision.LogicalName != logicalName {
			return fmt.Errorf("track remote indexeddb namespace store: physical name collision for %q", physicalName)
		}
	}

	rec := idb.Record{
		"id":            uuid.NewString(),
		"namespace_id":  namespaceID,
		"logical_name":  logicalName,
		"physical_name": physicalName,
		"created_at":    now,
		"deleted_at":    nil,
	}
	return store.Put(ctx, rec)
}

func (s *RemoteIndexedDBNamespaceService) MarkStoreDeleted(ctx context.Context, namespaceID, logicalName string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("mark remote indexeddb namespace store deleted: service is not configured")
	}
	rec, err := s.db.ObjectStore(StoreRemoteIndexedDBNamespaceStores).Index("by_namespace_logical").Get(ctx, []any{namespaceID, logicalName})
	if errors.Is(err, idb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mark remote indexeddb namespace store deleted: load: %w", err)
	}
	store := recordToRemoteIndexedDBNamespaceStore(rec)
	now := s.nowTime()
	store.DeletedAt = &now
	return s.db.ObjectStore(StoreRemoteIndexedDBNamespaceStores).Put(ctx, remoteIndexedDBNamespaceStoreRecord(store))
}

func (s *RemoteIndexedDBNamespaceService) ActivateRegistration(ctx context.Context, registrationID string, generation uint64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("activate remote indexeddb namespaces: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return fmt.Errorf("activate remote indexeddb namespaces: registration_id is required")
	}

	nsStore := s.db.ObjectStore(StoreRemoteIndexedDBNamespaces)
	recs, err := nsStore.Index("by_registration").GetAll(ctx, registrationID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("activate remote indexeddb namespaces: load: %w", err)
	}

	now := s.nowTime()
	for _, rec := range recs {
		ns := recordToRemoteIndexedDBNamespace(rec)
		if ns.Generation != generation {
			continue
		}
		ns.State = NamespaceActive
		ns.UpdatedAt = now
		if err := nsStore.Put(ctx, remoteIndexedDBNamespaceRecord(ns)); err != nil {
			return fmt.Errorf("activate remote indexeddb namespaces: store %q: %w", ns.ID, err)
		}
	}
	return nil
}

func (s *RemoteIndexedDBNamespaceService) RenewRegistration(ctx context.Context, registrationID string, generation uint64, leaseExpiresAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("renew remote indexeddb namespaces: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return fmt.Errorf("renew remote indexeddb namespaces: registration_id is required")
	}
	leaseExpiresAt = normalizedRemoteTime(leaseExpiresAt)
	if leaseExpiresAt.IsZero() {
		return fmt.Errorf("renew remote indexeddb namespaces: lease_expires_at is required")
	}

	nsStore := s.db.ObjectStore(StoreRemoteIndexedDBNamespaces)
	recs, err := nsStore.Index("by_registration").GetAll(ctx, registrationID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("renew remote indexeddb namespaces: load: %w", err)
	}

	now := s.nowTime()
	for _, rec := range recs {
		ns := recordToRemoteIndexedDBNamespace(rec)
		if ns.Generation != generation {
			continue
		}
		ns.LeaseExpiresAt = leaseExpiresAt
		ns.UpdatedAt = now
		if err := nsStore.Put(ctx, remoteIndexedDBNamespaceRecord(ns)); err != nil {
			return fmt.Errorf("renew remote indexeddb namespaces: store %q: %w", ns.ID, err)
		}
	}
	return nil
}

func (s *RemoteIndexedDBNamespaceService) MarkRegistrationCleanupPending(ctx context.Context, registrationID string, generation uint64, cleanupGrace time.Duration) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("mark remote indexeddb namespaces cleanup pending: service is not configured")
	}
	registrationID = strings.TrimSpace(registrationID)
	if registrationID == "" {
		return fmt.Errorf("mark remote indexeddb namespaces cleanup pending: registration_id is required")
	}

	nsStore := s.db.ObjectStore(StoreRemoteIndexedDBNamespaces)
	recs, err := nsStore.Index("by_registration").GetAll(ctx, registrationID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("mark remote indexeddb namespaces cleanup pending: load: %w", err)
	}

	now := s.nowTime()
	cleanupAfter := normalizedRemoteTime(now.Add(cleanupGrace))
	for _, rec := range recs {
		ns := recordToRemoteIndexedDBNamespace(rec)
		if ns.Generation != generation {
			continue
		}
		ns.State = NamespaceCleanupPending
		ns.CleanupAfter = cleanupAfter
		ns.UpdatedAt = now
		if err := nsStore.Put(ctx, remoteIndexedDBNamespaceRecord(ns)); err != nil {
			return fmt.Errorf("mark remote indexeddb namespaces cleanup pending: store %q: %w", ns.ID, err)
		}
	}
	return nil
}

func (s *RemoteIndexedDBNamespaceService) ClaimCleanup(ctx context.Context, namespaceID, holderID string, ttl time.Duration) (*RemoteIndexedDBNamespace, []*RemoteIndexedDBNamespaceStore, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: service is not configured")
	}
	namespaceID = strings.TrimSpace(namespaceID)
	if namespaceID == "" {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: namespace_id is required")
	}
	holderID = strings.TrimSpace(holderID)
	if holderID == "" {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: holder_id is required")
	}
	if ttl <= 0 {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: ttl must be positive")
	}

	nsStore := s.db.ObjectStore(StoreRemoteIndexedDBNamespaces)
	rec, err := nsStore.Get(ctx, namespaceID)
	if errors.Is(err, idb.ErrNotFound) {
		return nil, nil, ErrNotRegistered
	}
	if err != nil {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: load: %w", err)
	}
	ns := recordToRemoteIndexedDBNamespace(rec)
	now := s.nowTime()
	if ns.State != NamespaceCleanupPending {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: namespace is not cleanup_pending")
	}
	if ns.CleanupAfter.After(now) {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: cleanup grace has not elapsed")
	}
	if ns.CleanupHolder != "" && ns.CleanupLeaseExpiresAt.After(now) && ns.CleanupHolder != holderID {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: held by another worker")
	}

	ns.CleanupHolder = holderID
	ns.CleanupLeaseExpiresAt = normalizedRemoteTime(now.Add(ttl))
	ns.CleanupAttemptCount++
	ns.UpdatedAt = now
	if err := nsStore.Put(ctx, remoteIndexedDBNamespaceRecord(ns)); err != nil {
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: store: %w", err)
	}

	storeRecs, err := s.db.ObjectStore(StoreRemoteIndexedDBNamespaceStores).Index("by_namespace").GetAll(ctx, namespaceID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return ns, nil, nil
		}
		return nil, nil, fmt.Errorf("claim remote indexeddb namespace cleanup: load stores: %w", err)
	}
	stores := make([]*RemoteIndexedDBNamespaceStore, 0, len(storeRecs))
	for _, sr := range storeRecs {
		stores = append(stores, recordToRemoteIndexedDBNamespaceStore(sr))
	}
	return ns, stores, nil
}

func (s *RemoteIndexedDBNamespaceService) CompleteCleanup(ctx context.Context, namespaceID, holderID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("complete remote indexeddb namespace cleanup: service is not configured")
	}
	return s.updateCleanupHolder(ctx, namespaceID, holderID, func(ns *RemoteIndexedDBNamespace, now time.Time) {
		ns.State = NamespaceCleaned
		ns.CleanupHolder = ""
		ns.CleanupLeaseExpiresAt = time.Time{}
		ns.UpdatedAt = now
	})
}

func (s *RemoteIndexedDBNamespaceService) RecordCleanupFailure(ctx context.Context, namespaceID, holderID, errMessage string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("record remote indexeddb namespace cleanup failure: service is not configured")
	}
	return s.updateCleanupHolder(ctx, namespaceID, holderID, func(ns *RemoteIndexedDBNamespace, now time.Time) {
		ns.CleanupHolder = ""
		ns.CleanupLeaseExpiresAt = time.Time{}
		ns.LastCleanupError = sanitizeRemoteRegistrationError(errMessage)
		ns.UpdatedAt = now
	})
}

func (s *RemoteIndexedDBNamespaceService) updateCleanupHolder(ctx context.Context, namespaceID, holderID string, mutate func(*RemoteIndexedDBNamespace, time.Time)) error {
	namespaceID = strings.TrimSpace(namespaceID)
	holderID = strings.TrimSpace(holderID)
	rec, err := s.db.ObjectStore(StoreRemoteIndexedDBNamespaces).Get(ctx, namespaceID)
	if errors.Is(err, idb.ErrNotFound) {
		return ErrNotRegistered
	}
	if err != nil {
		return fmt.Errorf("update remote indexeddb namespace cleanup holder: load: %w", err)
	}
	ns := recordToRemoteIndexedDBNamespace(rec)
	now := s.nowTime()
	if ns.CleanupHolder != holderID {
		return fmt.Errorf("update remote indexeddb namespace cleanup holder: holder mismatch")
	}
	mutate(ns, now)
	return s.db.ObjectStore(StoreRemoteIndexedDBNamespaces).Put(ctx, remoteIndexedDBNamespaceRecord(ns))
}

func remoteIndexedDBNamespaceRecord(ns *RemoteIndexedDBNamespace) idb.Record {
	rec := idb.Record{
		"id":                       ns.ID,
		"registration_id":          ns.RegistrationID,
		"generation":               ns.Generation,
		"session_id":               ns.SessionID,
		"owner_subject_id":         ns.OwnerSubjectID,
		"app_name":                 ns.AppName,
		"provider_name":            ns.ProviderName,
		"database_name":            ns.DatabaseName,
		"state":                    string(ns.State),
		"lease_expires_at":         ns.LeaseExpiresAt,
		"cleanup_after":            ns.CleanupAfter,
		"created_at":               ns.CreatedAt,
		"updated_at":               ns.UpdatedAt,
		"cleanup_holder":           ns.CleanupHolder,
		"cleanup_lease_expires_at": ns.CleanupLeaseExpiresAt,
		"cleanup_attempt_count":    ns.CleanupAttemptCount,
		"last_cleanup_error":       ns.LastCleanupError,
	}
	if rec["cleanup_after"].(time.Time).IsZero() {
		rec["cleanup_after"] = nil
	}
	if rec["cleanup_lease_expires_at"].(time.Time).IsZero() {
		rec["cleanup_lease_expires_at"] = nil
	}
	return rec
}

func recordToRemoteIndexedDBNamespace(rec idb.Record) *RemoteIndexedDBNamespace {
	return &RemoteIndexedDBNamespace{
		ID:                    recString(rec, "id"),
		RegistrationID:        recString(rec, "registration_id"),
		Generation:            recUint64(rec, "generation"),
		SessionID:             recString(rec, "session_id"),
		OwnerSubjectID:        recString(rec, "owner_subject_id"),
		AppName:               recString(rec, "app_name"),
		ProviderName:          recString(rec, "provider_name"),
		DatabaseName:          recString(rec, "database_name"),
		State:                 RemoteIndexedDBNamespaceState(recString(rec, "state")),
		LeaseExpiresAt:        recTime(rec, "lease_expires_at"),
		CleanupAfter:          recTime(rec, "cleanup_after"),
		CreatedAt:             recTime(rec, "created_at"),
		UpdatedAt:             recTime(rec, "updated_at"),
		CleanupHolder:         recString(rec, "cleanup_holder"),
		CleanupLeaseExpiresAt: recTime(rec, "cleanup_lease_expires_at"),
		CleanupAttemptCount:   int(recUint64(rec, "cleanup_attempt_count")),
		LastCleanupError:      recString(rec, "last_cleanup_error"),
	}
}

func remoteIndexedDBNamespaceStoreRecord(store *RemoteIndexedDBNamespaceStore) idb.Record {
	rec := idb.Record{
		"id":            store.ID,
		"namespace_id":  store.NamespaceID,
		"logical_name":  store.LogicalName,
		"physical_name": store.PhysicalName,
		"created_at":    store.CreatedAt,
		"deleted_at":    nil,
	}
	if store.DeletedAt != nil {
		rec["deleted_at"] = *store.DeletedAt
	}
	return rec
}

func recordToRemoteIndexedDBNamespaceStore(rec idb.Record) *RemoteIndexedDBNamespaceStore {
	return &RemoteIndexedDBNamespaceStore{
		ID:           recString(rec, "id"),
		NamespaceID:  recString(rec, "namespace_id"),
		LogicalName:  recString(rec, "logical_name"),
		PhysicalName: recString(rec, "physical_name"),
		CreatedAt:    recTime(rec, "created_at"),
		DeletedAt:    recTimePtr(rec, "deleted_at"),
	}
}
