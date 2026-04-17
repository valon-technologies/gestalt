package coredata

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type ManagedIdentityMembershipService struct {
	store           indexeddb.ObjectStore
	canonicalGrants *ServiceAccountManagementGrantService
}

func NewManagedIdentityMembershipService(ds indexeddb.IndexedDB, canonicalGrants *ServiceAccountManagementGrantService) *ManagedIdentityMembershipService {
	return &ManagedIdentityMembershipService{
		store:           ds.ObjectStore(StoreManagedIdentityMemberships),
		canonicalGrants: canonicalGrants,
	}
}

func (s *ManagedIdentityMembershipService) UpsertMembership(ctx context.Context, membership *core.ManagedIdentityMembership) (*core.ManagedIdentityMembership, error) {
	if membership == nil || membership.IdentityID == "" || membership.UserID == "" {
		return nil, fmt.Errorf("upsert managed identity membership: identity_id and user_id are required")
	}

	now := time.Now()
	rec, err := s.store.Index("by_identity_user").Get(ctx, membership.IdentityID, membership.UserID)
	switch {
	case err == nil:
		existing := recordToManagedIdentityMembership(rec)
		existing.Email = membership.Email
		existing.Role = membership.Role
		existing.UpdatedAt = now
		if err := s.store.Put(ctx, managedIdentityMembershipRecord(existing)); err != nil {
			return nil, fmt.Errorf("update managed identity membership: %w", err)
		}
		if err := s.syncCanonicalGrant(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	case err != nil && err != indexeddb.ErrNotFound:
		return nil, fmt.Errorf("lookup managed identity membership: %w", err)
	}

	if membership.ID == "" {
		membership.ID = uuid.NewString()
	}
	membership.CreatedAt = now
	membership.UpdatedAt = now
	if err := s.store.Add(ctx, managedIdentityMembershipRecord(membership)); err != nil {
		return nil, fmt.Errorf("create managed identity membership: %w", err)
	}
	if err := s.syncCanonicalGrant(ctx, membership); err != nil {
		return nil, err
	}
	return membership, nil
}

func (s *ManagedIdentityMembershipService) GetMembership(ctx context.Context, identityID, userID string) (*core.ManagedIdentityMembership, error) {
	rec, err := s.store.Index("by_identity_user").Get(ctx, identityID, userID)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get managed identity membership: %w", err)
	}
	return recordToManagedIdentityMembership(rec), nil
}

func (s *ManagedIdentityMembershipService) ListMembershipsByIdentity(ctx context.Context, identityID string) ([]*core.ManagedIdentityMembership, error) {
	recs, err := s.store.Index("by_identity").GetAll(ctx, nil, identityID)
	if err != nil {
		return nil, fmt.Errorf("list managed identity memberships by identity: %w", err)
	}
	out := make([]*core.ManagedIdentityMembership, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToManagedIdentityMembership(rec))
	}
	return out, nil
}

func (s *ManagedIdentityMembershipService) ListMembershipsByUser(ctx context.Context, userID string) ([]*core.ManagedIdentityMembership, error) {
	recs, err := s.store.Index("by_user").GetAll(ctx, nil, userID)
	if err != nil {
		return nil, fmt.Errorf("list managed identity memberships by user: %w", err)
	}
	out := make([]*core.ManagedIdentityMembership, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToManagedIdentityMembership(rec))
	}
	return out, nil
}

func (s *ManagedIdentityMembershipService) DeleteMembership(ctx context.Context, identityID, userID string) error {
	deleted, err := s.store.Index("by_identity_user").Delete(ctx, identityID, userID)
	if err != nil {
		return fmt.Errorf("delete managed identity membership: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	if s.canonicalGrants != nil {
		if err := s.canonicalGrants.DeleteGrant(ctx, userID, identityID); err != nil && err != core.ErrNotFound {
			return fmt.Errorf("delete canonical service account management grant: %w", err)
		}
	}
	return nil
}

func (s *ManagedIdentityMembershipService) RestoreMembership(ctx context.Context, membership *core.ManagedIdentityMembership) error {
	if membership == nil || membership.ID == "" || membership.IdentityID == "" || membership.UserID == "" {
		return fmt.Errorf("restore managed identity membership: id, identity_id, and user_id are required")
	}
	if err := s.store.Put(ctx, managedIdentityMembershipRecord(membership)); err != nil {
		return fmt.Errorf("restore managed identity membership: %w", err)
	}
	if err := s.syncCanonicalGrant(ctx, membership); err != nil {
		return err
	}
	return nil
}

func (s *ManagedIdentityMembershipService) BackfillCanonicalGrants(ctx context.Context) error {
	if s.canonicalGrants == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list managed identity memberships for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		if err := s.syncCanonicalGrant(ctx, recordToManagedIdentityMembership(rec)); err != nil {
			return err
		}
	}
	return nil
}

func managedIdentityMembershipRecord(membership *core.ManagedIdentityMembership) indexeddb.Record {
	return indexeddb.Record{
		"id":          membership.ID,
		"identity_id": membership.IdentityID,
		"user_id":     membership.UserID,
		"email":       membership.Email,
		"role":        membership.Role,
		"created_at":  membership.CreatedAt,
		"updated_at":  membership.UpdatedAt,
	}
}

func recordToManagedIdentityMembership(rec indexeddb.Record) *core.ManagedIdentityMembership {
	return &core.ManagedIdentityMembership{
		ID:         recString(rec, "id"),
		IdentityID: recString(rec, "identity_id"),
		UserID:     recString(rec, "user_id"),
		Email:      recString(rec, "email"),
		Role:       recString(rec, "role"),
		CreatedAt:  recTime(rec, "created_at"),
		UpdatedAt:  recTime(rec, "updated_at"),
	}
}

func (s *ManagedIdentityMembershipService) syncCanonicalGrant(ctx context.Context, membership *core.ManagedIdentityMembership) error {
	if s.canonicalGrants == nil || membership == nil || membership.UserID == "" || membership.IdentityID == "" {
		return nil
	}
	if _, err := s.canonicalGrants.UpsertGrant(ctx, &core.ServiceAccountManagementGrant{
		MemberPrincipalID:               membership.UserID,
		TargetServiceAccountPrincipalID: membership.IdentityID,
		Role:                            membership.Role,
		CreatedAt:                       membership.CreatedAt,
		UpdatedAt:                       membership.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical service account management grant %q/%q: %w", membership.UserID, membership.IdentityID, err)
	}
	return nil
}
