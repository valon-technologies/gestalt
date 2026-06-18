package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type UserService struct {
	store idb.ObjectStore
}

func NewUserService(ds indexeddb.IndexedDB) *UserService {
	return &UserService{store: ds.ObjectStore(StoreUsers)}
}

func (s *UserService) GetUser(ctx context.Context, id string) (*core.User, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if err == idb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return recordToUser(rec), nil
}

func (s *UserService) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	return s.FindOrCreateUserWithName(ctx, email, "")
}

func (s *UserService) FindOrCreateUserWithName(ctx context.Context, email, name string) (*core.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("find user: email is required")
	}

	user, err := s.findUserByNormalizedEmail(ctx, email)
	switch {
	case err == nil:
		return s.maybeUpdateDisplayName(ctx, user, name)
	case !errors.Is(err, core.ErrNotFound):
		return nil, err
	}

	now := time.Now()
	newRec := idb.Record{
		"id":               uuid.New().String(),
		"email":            email,
		"normalized_email": email,
		"display_name":     strings.TrimSpace(name),
		"created_at":       now,
		"updated_at":       now,
	}
	if err := s.store.Add(ctx, newRec); err != nil {
		user, retryErr := s.findUserByNormalizedEmail(ctx, email)
		if retryErr != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return s.maybeUpdateDisplayName(ctx, user, name)
	}
	return recordToUser(newRec), nil
}

func (s *UserService) maybeUpdateDisplayName(ctx context.Context, user *core.User, displayName string) (*core.User, error) {
	displayName = strings.TrimSpace(displayName)
	if user == nil || displayName == "" || strings.TrimSpace(user.DisplayName) == displayName {
		return user, nil
	}
	rec, err := s.store.Get(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("update user display name: %w", err)
	}
	now := time.Now()
	rec["display_name"] = displayName
	rec["updated_at"] = now
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("update user display name: %w", err)
	}
	updated := *user
	updated.DisplayName = displayName
	updated.UpdatedAt = now
	return &updated, nil
}

func (s *UserService) FindUserByEmail(ctx context.Context, email string) (*core.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("find user: email is required")
	}
	return s.findUserByNormalizedEmail(ctx, email)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *UserService) findUserByNormalizedEmail(ctx context.Context, normalizedEmail string) (*core.User, error) {
	recs, err := s.store.Index("by_normalized_email").GetAll(ctx, normalizedEmail)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}
	if len(recs) > 1 {
		return nil, fmt.Errorf("find user: ambiguous duplicate users for %q", normalizedEmail)
	}
	return recordToUser(recs[0]), nil
}

func recordToUser(rec idb.Record) *core.User {
	return &core.User{
		ID:          recString(rec, "id"),
		Email:       recString(rec, "email"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
