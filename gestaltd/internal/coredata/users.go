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

var ErrUserEmailConflict = errors.New("user email is already linked to another user")

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
	newRec := newUserRecord(uuid.NewString(), email, name, now)
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

// LinkUserInTransaction links an externally managed identity to a Gestalt
// user while participating in the caller's transaction. An empty userID
// reuses the single user with the normalized email or creates one.
func LinkUserInTransaction(ctx context.Context, tx idb.Transaction, userID, email, displayName string, now time.Time) (*core.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("link user: email is required")
	}
	store := tx.ObjectStore(StoreUsers)
	records, err := store.Index("by_normalized_email").GetAll(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("link user: find normalized email: %w", err)
	}
	if len(records) > 1 || len(records) == 1 && userID != "" && recString(records[0], "id") != userID {
		return nil, fmt.Errorf("%w: %s", ErrUserEmailConflict, email)
	}
	if userID == "" {
		if len(records) == 1 {
			userID = recString(records[0], "id")
		} else {
			rec := newUserRecord(uuid.NewString(), email, displayName, now)
			if err := store.Add(ctx, rec); err != nil {
				return nil, fmt.Errorf("link user: create: %w", err)
			}
			return recordToUser(rec), nil
		}
	}
	rec, err := store.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("link user: get %q: %w", userID, err)
	}
	rec["email"] = email
	rec["normalized_email"] = email
	if displayName = strings.TrimSpace(displayName); displayName != "" {
		rec["display_name"] = displayName
	}
	rec["updated_at"] = now
	if err := store.Put(ctx, rec); err != nil {
		if errors.Is(err, idb.ErrAlreadyExists) {
			return nil, fmt.Errorf("%w: %s", ErrUserEmailConflict, email)
		}
		return nil, fmt.Errorf("link user: update: %w", err)
	}
	return recordToUser(rec), nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func newUserRecord(id, email, displayName string, now time.Time) idb.Record {
	return idb.Record{
		"id":               id,
		"email":            email,
		"normalized_email": email,
		"display_name":     strings.TrimSpace(displayName),
		"created_at":       now,
		"updated_at":       now,
	}
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
