package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type ManagedIdentityMembershipService struct {
	store           indexeddb.ObjectStore
	canonicalGrants *IdentityManagementGrantService
	users           *UserService
}

func NewManagedIdentityMembershipService(ds indexeddb.IndexedDB, canonicalGrants *IdentityManagementGrantService, users *UserService) *ManagedIdentityMembershipService {
	return &ManagedIdentityMembershipService{
		store:           ds.ObjectStore(StoreManagedIdentityMemberships),
		canonicalGrants: canonicalGrants,
		users:           users,
	}
}

func (s *ManagedIdentityMembershipService) UpsertMembership(ctx context.Context, membership *core.ManagedIdentityMembership) (*core.ManagedIdentityMembership, error) {
	if membership == nil || membership.IdentityID == "" || membership.SubjectID == "" {
		return nil, fmt.Errorf("upsert managed identity membership: identity_id and subject_id are required")
	}
	membership.SubjectID = strings.TrimSpace(membership.SubjectID)
	if managedIdentityMembershipUserIDFromSubjectID(membership.SubjectID) == "" {
		return nil, fmt.Errorf("upsert managed identity membership: subject_id must be a canonical user subject")
	}

	now := time.Now()
	rec, err := s.store.Index("by_identity_subject").Get(ctx, membership.IdentityID, membership.SubjectID)
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

func (s *ManagedIdentityMembershipService) GetMembership(ctx context.Context, identityID, subjectID string) (*core.ManagedIdentityMembership, error) {
	rec, err := s.store.Index("by_identity_subject").Get(ctx, identityID, strings.TrimSpace(subjectID))
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

func (s *ManagedIdentityMembershipService) ListMembershipsBySubject(ctx context.Context, subjectID string) ([]*core.ManagedIdentityMembership, error) {
	recs, err := s.store.Index("by_subject").GetAll(ctx, nil, strings.TrimSpace(subjectID))
	if err != nil {
		return nil, fmt.Errorf("list managed identity memberships by subject: %w", err)
	}
	out := make([]*core.ManagedIdentityMembership, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToManagedIdentityMembership(rec))
	}
	return out, nil
}

func (s *ManagedIdentityMembershipService) DeleteMembership(ctx context.Context, identityID, subjectID string) error {
	subjectID = strings.TrimSpace(subjectID)
	deleted, err := s.store.Index("by_identity_subject").Delete(ctx, identityID, subjectID)
	if err != nil {
		return fmt.Errorf("delete managed identity membership: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	if s.canonicalGrants != nil {
		managerIdentityID, resolveErr := s.resolveManagerIdentityID(ctx, subjectID)
		if resolveErr == nil {
			if err := s.canonicalGrants.DeleteGrant(ctx, managerIdentityID, identityID); err != nil && err != core.ErrNotFound {
				return fmt.Errorf("delete canonical identity management grant: %w", err)
			}
		}
	}
	return nil
}

func (s *ManagedIdentityMembershipService) RestoreMembership(ctx context.Context, membership *core.ManagedIdentityMembership) error {
	if membership == nil || membership.ID == "" || membership.IdentityID == "" || membership.SubjectID == "" {
		return fmt.Errorf("restore managed identity membership: id, identity_id, and subject_id are required")
	}
	membership.SubjectID = strings.TrimSpace(membership.SubjectID)
	if managedIdentityMembershipUserIDFromSubjectID(membership.SubjectID) == "" {
		return fmt.Errorf("restore managed identity membership: subject_id must be a canonical user subject")
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
		"subject_id":  membership.SubjectID,
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
		SubjectID:  recString(rec, "subject_id"),
		Email:      recString(rec, "email"),
		Role:       recString(rec, "role"),
		CreatedAt:  recTime(rec, "created_at"),
		UpdatedAt:  recTime(rec, "updated_at"),
	}
}

func (s *ManagedIdentityMembershipService) resolveManagerIdentityID(ctx context.Context, subjectID string) (string, error) {
	userID := managedIdentityMembershipUserIDFromSubjectID(subjectID)
	if userID == "" {
		return "", core.ErrNotFound
	}
	if s.users == nil {
		return userID, nil
	}
	return s.users.CanonicalIdentityIDForUser(ctx, userID)
}

func (s *ManagedIdentityMembershipService) syncCanonicalGrant(ctx context.Context, membership *core.ManagedIdentityMembership) error {
	if s.canonicalGrants == nil || membership == nil || membership.SubjectID == "" || membership.IdentityID == "" {
		return nil
	}
	managerIdentityID, resolveErr := s.resolveManagerIdentityID(ctx, membership.SubjectID)
	if resolveErr != nil {
		if errors.Is(resolveErr, core.ErrNotFound) {
			return nil
		}
		return resolveErr
	}
	if _, err := s.canonicalGrants.UpsertGrant(ctx, &core.IdentityManagementGrant{
		ManagerIdentityID: managerIdentityID,
		TargetIdentityID:  membership.IdentityID,
		Role:              membership.Role,
		CreatedAt:         membership.CreatedAt,
		UpdatedAt:         membership.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical identity management grant %q/%q: %w", managerIdentityID, membership.IdentityID, err)
	}
	return nil
}

// Keep this local: importing internal/principal here creates a cycle via
// principal/resolver.go -> coredata.
func managedIdentityMembershipUserIDFromSubjectID(subjectID string) string {
	subjectID = strings.TrimSpace(subjectID)
	if !strings.HasPrefix(subjectID, "user:") {
		return ""
	}
	return strings.TrimPrefix(subjectID, "user:")
}
