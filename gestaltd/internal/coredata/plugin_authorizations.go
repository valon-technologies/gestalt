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

type PluginAuthorizationMembership struct {
	ID        string
	Plugin    string
	UserID    string
	Email     string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PluginAuthorizationService struct {
	store           indexeddb.ObjectStore
	canonicalAccess *IdentityPluginAccessService
	users           *UserService
}

func NewPluginAuthorizationService(ds indexeddb.IndexedDB, canonicalAccess *IdentityPluginAccessService, users *UserService) *PluginAuthorizationService {
	return &PluginAuthorizationService{
		store:           ds.ObjectStore(StorePluginAuthorizationMemberships),
		canonicalAccess: canonicalAccess,
		users:           users,
	}
}

func (s *PluginAuthorizationService) ListPluginAuthorizations(ctx context.Context) ([]*PluginAuthorizationMembership, error) {
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list plugin authorizations: %w", err)
	}
	out := make([]*PluginAuthorizationMembership, len(recs))
	for i, rec := range recs {
		out[i] = recordToPluginAuthorizationMembership(rec)
	}
	return out, nil
}

func (s *PluginAuthorizationService) ListPluginAuthorizationsByPlugin(ctx context.Context, plugin string) ([]*PluginAuthorizationMembership, error) {
	plugin = strings.TrimSpace(plugin)
	if plugin == "" {
		return nil, fmt.Errorf("list plugin authorizations: plugin is required")
	}
	recs, err := s.store.Index("by_plugin").GetAll(ctx, nil, plugin)
	if err != nil {
		return nil, fmt.Errorf("list plugin authorizations by plugin: %w", err)
	}
	out := make([]*PluginAuthorizationMembership, len(recs))
	for i, rec := range recs {
		out[i] = recordToPluginAuthorizationMembership(rec)
	}
	return out, nil
}

func (s *PluginAuthorizationService) UpsertPluginAuthorization(ctx context.Context, membership *PluginAuthorizationMembership) (*PluginAuthorizationMembership, error) {
	if membership == nil {
		return nil, fmt.Errorf("upsert plugin authorization: membership is required")
	}

	plugin := strings.TrimSpace(membership.Plugin)
	userID := strings.TrimSpace(membership.UserID)
	email := emailutil.Normalize(membership.Email)
	role := strings.TrimSpace(membership.Role)
	switch {
	case plugin == "":
		return nil, fmt.Errorf("upsert plugin authorization: plugin is required")
	case userID == "":
		return nil, fmt.Errorf("upsert plugin authorization: userID is required")
	case email == "":
		return nil, fmt.Errorf("upsert plugin authorization: email is required")
	case role == "":
		return nil, fmt.Errorf("upsert plugin authorization: role is required")
	}

	id := pluginAuthorizationRecordID(plugin, userID)
	now := time.Now()
	createdAt := now
	if existing, err := s.store.Get(ctx, id); err == nil {
		createdAt = recTime(existing, "created_at")
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert plugin authorization: %w", err)
	}

	rec := indexeddb.Record{
		"id":         id,
		"plugin":     plugin,
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
		return nil, fmt.Errorf("upsert plugin authorization: %w", err)
	}
	if err := s.syncCanonicalAccess(ctx, recordToPluginAuthorizationMembership(rec)); err != nil {
		return nil, err
	}
	return recordToPluginAuthorizationMembership(rec), nil
}

func (s *PluginAuthorizationService) DeletePluginAuthorization(ctx context.Context, plugin, userID string) error {
	plugin = strings.TrimSpace(plugin)
	userID = strings.TrimSpace(userID)
	if plugin == "" || userID == "" {
		return fmt.Errorf("delete plugin authorization: plugin and userID are required")
	}
	if err := s.store.Delete(ctx, pluginAuthorizationRecordID(plugin, userID)); err != nil {
		if err == indexeddb.ErrNotFound {
			return core.ErrNotFound
		}
		return fmt.Errorf("delete plugin authorization: %w", err)
	}
	if s.canonicalAccess != nil {
		identityID, resolveErr := s.resolveIdentityID(ctx, userID)
		if resolveErr == nil {
			if err := s.canonicalAccess.DeleteAccess(ctx, identityID, plugin); err != nil && err != core.ErrNotFound {
				return fmt.Errorf("delete canonical identity plugin access: %w", err)
			}
		}
	}
	return nil
}

func (s *PluginAuthorizationService) BackfillCanonicalAccess(ctx context.Context) error {
	if s.canonicalAccess == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list plugin authorizations for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		if err := s.syncCanonicalAccess(ctx, recordToPluginAuthorizationMembership(rec)); err != nil {
			return err
		}
	}
	return nil
}

func pluginAuthorizationRecordID(plugin, userID string) string {
	return plugin + "\x00" + userID
}

func recordToPluginAuthorizationMembership(rec indexeddb.Record) *PluginAuthorizationMembership {
	return &PluginAuthorizationMembership{
		ID:        recString(rec, "id"),
		Plugin:    recString(rec, "plugin"),
		UserID:    recString(rec, "user_id"),
		Email:     recString(rec, "email"),
		Role:      recString(rec, "role"),
		CreatedAt: recTime(rec, "created_at"),
		UpdatedAt: recTime(rec, "updated_at"),
	}
}

func (s *PluginAuthorizationService) resolveIdentityID(ctx context.Context, userID string) (string, error) {
	if s.users == nil {
		return userID, nil
	}
	return s.users.CanonicalIdentityIDForUser(ctx, userID)
}

func (s *PluginAuthorizationService) syncCanonicalAccess(ctx context.Context, membership *PluginAuthorizationMembership) error {
	if s.canonicalAccess == nil || membership == nil || membership.UserID == "" || membership.Plugin == "" {
		return nil
	}
	identityID, resolveErr := s.resolveIdentityID(ctx, membership.UserID)
	if resolveErr != nil {
		if errors.Is(resolveErr, core.ErrNotFound) {
			return nil
		}
		return resolveErr
	}
	if _, err := s.canonicalAccess.UpsertAccess(ctx, &core.IdentityPluginAccess{
		IdentityID:          identityID,
		Plugin:              membership.Plugin,
		InvokeAllOperations: true,
		CreatedAt:           membership.CreatedAt,
		UpdatedAt:           membership.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical identity plugin access %q/%q: %w", identityID, membership.Plugin, err)
	}
	return nil
}
