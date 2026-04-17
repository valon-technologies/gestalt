package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/emailutil"
)

type AdminAuthorizationMembership struct {
	ID        string
	UserID    string
	Email     string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AdminAuthorizationService struct {
	store          indexeddb.ObjectStore
	workspaceRoles *WorkspaceRoleService
	users          *UserService
}

func NewAdminAuthorizationService(ds indexeddb.IndexedDB, workspaceRoles *WorkspaceRoleService, users *UserService) *AdminAuthorizationService {
	return &AdminAuthorizationService{
		store:          ds.ObjectStore(StoreAdminAuthorizationMemberships),
		workspaceRoles: workspaceRoles,
		users:          users,
	}
}

func (s *AdminAuthorizationService) ListAdminAuthorizations(ctx context.Context) ([]*AdminAuthorizationMembership, error) {
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list admin authorizations: %w", err)
	}
	out := make([]*AdminAuthorizationMembership, len(recs))
	for i, rec := range recs {
		out[i] = recordToAdminAuthorizationMembership(rec)
	}
	return out, nil
}

func (s *AdminAuthorizationService) UpsertAdminAuthorization(ctx context.Context, membership *AdminAuthorizationMembership) (*AdminAuthorizationMembership, error) {
	if membership == nil {
		return nil, fmt.Errorf("upsert admin authorization: membership is required")
	}

	userID := strings.TrimSpace(membership.UserID)
	email := emailutil.Normalize(membership.Email)
	role := strings.TrimSpace(membership.Role)
	switch {
	case userID == "":
		return nil, fmt.Errorf("upsert admin authorization: userID is required")
	case email == "":
		return nil, fmt.Errorf("upsert admin authorization: email is required")
	case role == "":
		return nil, fmt.Errorf("upsert admin authorization: role is required")
	}

	id := adminAuthorizationRecordID(userID)
	now := time.Now()
	createdAt := now
	previousRole := ""
	if existing, err := s.store.Get(ctx, id); err == nil {
		createdAt = recTime(existing, "created_at")
		previousRole = strings.TrimSpace(recString(existing, "role"))
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert admin authorization: %w", err)
	}

	rec := indexeddb.Record{
		"id":         id,
		"user_id":    userID,
		"email":      email,
		"role":       role,
		"created_at": createdAt,
		"updated_at": now,
	}
	if createdAt.IsZero() {
		rec["created_at"] = now
	}

	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert admin authorization: %w", err)
	}
	if s.workspaceRoles != nil && previousRole != "" && previousRole != role {
		identityID, resolveErr := s.resolveIdentityID(ctx, userID)
		if resolveErr == nil {
			if err := s.workspaceRoles.DeleteRole(ctx, identityID, previousRole); err != nil && err != core.ErrNotFound {
				return nil, fmt.Errorf("delete stale canonical workspace role: %w", err)
			}
		}
	}
	if err := s.syncWorkspaceRole(ctx, recordToAdminAuthorizationMembership(rec)); err != nil {
		return nil, err
	}
	return recordToAdminAuthorizationMembership(rec), nil
}

func (s *AdminAuthorizationService) DeleteAdminAuthorization(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("delete admin authorization: userID is required")
	}
	existing, err := s.store.Get(ctx, adminAuthorizationRecordID(userID))
	if err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("delete admin authorization: %w", err)
	}
	if err := s.store.Delete(ctx, adminAuthorizationRecordID(userID)); err != nil {
		if err == indexeddb.ErrNotFound {
			return core.ErrNotFound
		}
		return fmt.Errorf("delete admin authorization: %w", err)
	}
	if s.workspaceRoles != nil {
		role := core.WorkspaceRoleAdmin
		if existing != nil && recString(existing, "role") != "" {
			role = recString(existing, "role")
		}
		identityID, resolveErr := s.resolveIdentityID(ctx, userID)
		if resolveErr == nil {
			if err := s.workspaceRoles.DeleteRole(ctx, identityID, role); err != nil && err != core.ErrNotFound {
				return fmt.Errorf("delete canonical workspace role: %w", err)
			}
		}
	}
	return nil
}

func (s *AdminAuthorizationService) BackfillCanonicalWorkspaceRoles(ctx context.Context) error {
	if s.workspaceRoles == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list admin authorizations for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		if err := s.syncWorkspaceRole(ctx, recordToAdminAuthorizationMembership(rec)); err != nil {
			return err
		}
	}
	return nil
}

func adminAuthorizationRecordID(userID string) string {
	return userID
}

func recordToAdminAuthorizationMembership(rec indexeddb.Record) *AdminAuthorizationMembership {
	return &AdminAuthorizationMembership{
		ID:        recString(rec, "id"),
		UserID:    recString(rec, "user_id"),
		Email:     recString(rec, "email"),
		Role:      recString(rec, "role"),
		CreatedAt: recTime(rec, "created_at"),
		UpdatedAt: recTime(rec, "updated_at"),
	}
}

func (s *AdminAuthorizationService) resolveIdentityID(ctx context.Context, userID string) (string, error) {
	if s.users == nil {
		return userID, nil
	}
	return s.users.CanonicalIdentityIDForUser(ctx, userID)
}

func (s *AdminAuthorizationService) syncWorkspaceRole(ctx context.Context, membership *AdminAuthorizationMembership) error {
	if s.workspaceRoles == nil || membership == nil || membership.UserID == "" || membership.Role == "" {
		return nil
	}
	identityID, resolveErr := s.resolveIdentityID(ctx, membership.UserID)
	if resolveErr != nil {
		if errors.Is(resolveErr, core.ErrNotFound) {
			return nil
		}
		return resolveErr
	}
	if _, err := s.workspaceRoles.UpsertRole(ctx, &core.WorkspaceRole{
		IdentityID: identityID,
		Role:       membership.Role,
		CreatedAt:  membership.CreatedAt,
		UpdatedAt:  membership.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical workspace role %q/%q: %w", identityID, membership.Role, err)
	}
	return nil
}
