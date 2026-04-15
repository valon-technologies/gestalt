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
	store indexeddb.ObjectStore
}

func NewManagedIdentityService(ds indexeddb.IndexedDB) *ManagedIdentityService {
	return &ManagedIdentityService{store: ds.ObjectStore(StoreManagedIdentities)}
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
		"id":           identity.ID,
		"display_name": identity.DisplayName,
		"created_at":   identity.CreatedAt,
		"updated_at":   identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("create managed identity: %w", err)
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
	if identity.UpdatedAt.IsZero() {
		identity.UpdatedAt = time.Now()
	}
	if err := s.store.Put(ctx, indexeddb.Record{
		"id":           identity.ID,
		"display_name": identity.DisplayName,
		"created_at":   identity.CreatedAt,
		"updated_at":   identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("update managed identity: %w", err)
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

func recordToManagedIdentity(rec indexeddb.Record) *core.ManagedIdentity {
	return &core.ManagedIdentity{
		ID:          recString(rec, "id"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
