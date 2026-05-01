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

type UserService struct {
	store indexeddb.ObjectStore
}

func NewUserService(ds indexeddb.IndexedDB) *UserService {
	return &UserService{store: ds.ObjectStore(StoreUsers)}
}

func (s *UserService) GetUser(ctx context.Context, id string) (*core.User, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return recordToUser(rec), nil
}

func (s *UserService) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return nil, fmt.Errorf("find user: email is required")
	}

	user, err := s.findUserByNormalizedEmail(ctx, email)
	switch {
	case err == nil:
		return user, nil
	case !errors.Is(err, core.ErrNotFound):
		return nil, err
	}

	now := time.Now()
	newRec := indexeddb.Record{
		"id":               uuid.New().String(),
		"email":            email,
		"normalized_email": email,
		"display_name":     "",
		"created_at":       now,
		"updated_at":       now,
	}
	if err := s.store.Add(ctx, newRec); err != nil {
		user, retryErr := s.findUserByNormalizedEmail(ctx, email)
		if retryErr != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	return recordToUser(newRec), nil
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
	recs, err := s.store.Index("by_normalized_email").GetAll(ctx, nil, normalizedEmail)
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

func recordToUser(rec indexeddb.Record) *core.User {
	return &core.User{
		ID:          recString(rec, "id"),
		Email:       recString(rec, "email"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
