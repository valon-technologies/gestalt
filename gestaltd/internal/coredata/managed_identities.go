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
	store           indexeddb.ObjectStore
	principals      *PrincipalService
	serviceAccounts *ServiceAccountService
}

func NewManagedIdentityService(ds indexeddb.IndexedDB, principals *PrincipalService, serviceAccounts *ServiceAccountService) *ManagedIdentityService {
	return &ManagedIdentityService{
		store:           ds.ObjectStore(StoreManagedIdentities),
		principals:      principals,
		serviceAccounts: serviceAccounts,
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
		"id":           identity.ID,
		"display_name": identity.DisplayName,
		"created_at":   identity.CreatedAt,
		"updated_at":   identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("create managed identity: %w", err)
	}
	if err := s.syncCanonicalServiceAccount(ctx, identity); err != nil {
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
	if err := s.syncCanonicalServiceAccount(ctx, identity); err != nil {
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
	if s.serviceAccounts != nil {
		if err := s.serviceAccounts.DeleteServiceAccount(ctx, id); err != nil && err != core.ErrNotFound {
			return fmt.Errorf("delete canonical service account: %w", err)
		}
	}
	if s.principals != nil {
		if err := s.principals.DeletePrincipal(ctx, id); err != nil && err != core.ErrNotFound {
			return fmt.Errorf("delete canonical principal: %w", err)
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

func (s *ManagedIdentityService) BackfillCanonicalServiceAccounts(ctx context.Context) error {
	if s.principals == nil || s.serviceAccounts == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list managed identities for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		if err := s.syncCanonicalServiceAccount(ctx, recordToManagedIdentity(rec)); err != nil {
			return err
		}
	}
	return nil
}

func recordToManagedIdentity(rec indexeddb.Record) *core.ManagedIdentity {
	return &core.ManagedIdentity{
		ID:          recString(rec, "id"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}

func (s *ManagedIdentityService) syncCanonicalServiceAccount(ctx context.Context, identity *core.ManagedIdentity) error {
	if s.principals == nil || s.serviceAccounts == nil || identity == nil || identity.ID == "" {
		return nil
	}
	displayName := identity.DisplayName
	if displayName == "" {
		displayName = identity.ID
	}
	if _, err := s.principals.UpsertPrincipal(ctx, &core.Principal{
		ID:          identity.ID,
		Kind:        core.PrincipalKindServiceAccount,
		Status:      principalStatusActive,
		DisplayName: displayName,
		CreatedAt:   identity.CreatedAt,
		UpdatedAt:   identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical service-account principal %q: %w", identity.ID, err)
	}
	if _, err := s.serviceAccounts.UpsertServiceAccount(ctx, &core.ServiceAccount{
		PrincipalID: identity.ID,
		Name:        identity.ID,
		Description: identity.DisplayName,
		CreatedAt:   identity.CreatedAt,
		UpdatedAt:   identity.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical service account %q: %w", identity.ID, err)
	}
	return nil
}
