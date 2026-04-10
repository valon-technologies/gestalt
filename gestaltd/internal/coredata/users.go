package coredata

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/datastore"
)

type UserService struct {
	store datastore.ObjectStore
}

func NewUserService(ds datastore.Datastore) *UserService {
	return &UserService{store: ds.ObjectStore(StoreUsers)}
}

func (s *UserService) GetUser(ctx context.Context, id string) (*core.User, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if err == datastore.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return recordToUser(rec), nil
}

func (s *UserService) FindOrCreateUser(ctx context.Context, email string) (*core.User, error) {
	rec, err := s.store.Index("by_email").Get(ctx, email)
	if err == nil {
		return recordToUser(rec), nil
	}
	if err != datastore.ErrNotFound {
		return nil, fmt.Errorf("find user: %w", err)
	}
	now := time.Now()
	newRec := datastore.Record{
		"id":           uuid.New().String(),
		"email":        email,
		"display_name": "",
		"created_at":   now,
		"updated_at":   now,
	}
	if err := s.store.Add(ctx, newRec); err != nil {
		rec, retryErr := s.store.Index("by_email").Get(ctx, email)
		if retryErr != nil {
			return nil, fmt.Errorf("create user: %w", err)
		}
		return recordToUser(rec), nil
	}
	return recordToUser(newRec), nil
}

func recordToUser(rec datastore.Record) *core.User {
	return &core.User{
		ID:          recString(rec, "id"),
		Email:       recString(rec, "email"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
