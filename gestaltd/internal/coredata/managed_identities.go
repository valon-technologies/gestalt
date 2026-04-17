package coredata

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type ManagedIdentityService struct {
	store      indexeddb.ObjectStore
	identities *IdentityService
}

func NewManagedIdentityService(ds indexeddb.IndexedDB, identities *IdentityService) *ManagedIdentityService {
	return &ManagedIdentityService{
		store:      ds.ObjectStore(StoreManagedIdentities),
		identities: identities,
	}
}

func (s *ManagedIdentityService) CreateIdentity(ctx context.Context, identity *core.ManagedIdentity) error {
	if identity.ID == "" {
		identity.ID = uuid.NewString()
	}
	now := time.Now()
	if identity.CreatedAt.IsZero() {
		identity.CreatedAt = now
	}
	if identity.UpdatedAt.IsZero() {
		identity.UpdatedAt = identity.CreatedAt
	}
	if err := s.store.Add(ctx, indexeddb.Record{
		"id":                     identity.ID,
		"display_name":           identity.DisplayName,
		"created_by_identity_id": identity.CreatedByIdentityID,
		"created_at":             identity.CreatedAt,
		"updated_at":             identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("create managed identity: %w", err)
	}
	if err := s.syncCanonicalIdentity(ctx, identity); err != nil {
		return err
	}
	return nil
}

func (s *ManagedIdentityService) GetIdentity(ctx context.Context, id string) (*core.ManagedIdentity, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get managed identity: %w", err)
	}
	return recordToManagedIdentity(rec), nil
}

func (s *ManagedIdentityService) UpdateIdentity(ctx context.Context, identity *core.ManagedIdentity) error {
	if identity == nil || identity.ID == "" {
		return fmt.Errorf("update managed identity: id is required")
	}
	existing, err := s.GetIdentity(ctx, identity.ID)
	if err != nil {
		return err
	}
	identity.CreatedAt = existing.CreatedAt
	if identity.CreatedByIdentityID == "" {
		identity.CreatedByIdentityID = existing.CreatedByIdentityID
	}
	if identity.UpdatedAt.IsZero() {
		identity.UpdatedAt = time.Now()
	}
	if err := s.store.Put(ctx, indexeddb.Record{
		"id":                     identity.ID,
		"display_name":           identity.DisplayName,
		"created_by_identity_id": identity.CreatedByIdentityID,
		"created_at":             identity.CreatedAt,
		"updated_at":             identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("update managed identity: %w", err)
	}
	if err := s.syncCanonicalIdentity(ctx, identity); err != nil {
		return err
	}
	return nil
}

func (s *ManagedIdentityService) DeleteIdentity(ctx context.Context, id string) error {
	if _, err := s.GetIdentity(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete managed identity: %w", err)
	}
	if s.identities != nil {
		if err := s.identities.DeleteIdentity(ctx, id); err != nil && err != core.ErrNotFound {
			return fmt.Errorf("delete canonical identity: %w", err)
		}
	}
	return nil
}

func (s *ManagedIdentityService) ListIdentitiesByIDs(ctx context.Context, ids []string) ([]*core.ManagedIdentity, error) {
	out := make([]*core.ManagedIdentity, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		identity, err := s.GetIdentity(ctx, id)
		if err != nil {
			if err == core.ErrNotFound {
				continue
			}
			return nil, err
		}
		out = append(out, identity)
	}
	return out, nil
}

func (s *ManagedIdentityService) BackfillCanonicalIdentities(ctx context.Context) error {
	if s.identities == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list managed identities for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		if err := s.syncCanonicalIdentity(ctx, recordToManagedIdentity(rec)); err != nil {
			return err
		}
	}
	return nil
}

func recordToManagedIdentity(rec indexeddb.Record) *core.ManagedIdentity {
	return &core.ManagedIdentity{
		ID:                  recString(rec, "id"),
		DisplayName:         recString(rec, "display_name"),
		CreatedByIdentityID: recString(rec, "created_by_identity_id"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
}

func (s *ManagedIdentityService) syncCanonicalIdentity(ctx context.Context, identity *core.ManagedIdentity) error {
	if s.identities == nil || identity == nil || identity.ID == "" {
		return nil
	}
	displayName := identity.DisplayName
	if displayName == "" {
		displayName = identity.ID
	}
	if _, err := s.identities.UpsertIdentity(ctx, &core.Identity{
		ID:                  identity.ID,
		Status:              identityStatusActive,
		DisplayName:         displayName,
		CreatedByIdentityID: identity.CreatedByIdentityID,
		MetadataJSON:        legacyIdentityMetadataJSON("service_account", nil),
		CreatedAt:           identity.CreatedAt,
		UpdatedAt:           identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical identity %q: %w", identity.ID, err)
	}
	return nil
}
