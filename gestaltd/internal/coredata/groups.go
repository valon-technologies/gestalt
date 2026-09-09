package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

// Group is the persisted id → display-name mapping for an authorization group.
type Group struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type GroupService struct {
	store idb.ObjectStore
}

func NewGroupService(ds indexeddb.IndexedDB) *GroupService {
	return &GroupService{store: ds.ObjectStore(StoreGroups)}
}

// DisplayName returns the stored label for groupID, or "" when none exists.
func (s *GroupService) DisplayName(ctx context.Context, groupID string) (string, error) {
	group, err := s.Get(ctx, groupID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(group.DisplayName), nil
}

func (s *GroupService) Get(ctx context.Context, groupID string) (*Group, error) {
	if s == nil {
		return nil, fmt.Errorf("get group: service is not configured")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, fmt.Errorf("get group: id is required")
	}
	rec, err := s.store.Get(ctx, groupID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get group: %w", err)
	}
	return recordToGroup(rec), nil
}

func (s *GroupService) UpsertDisplayName(ctx context.Context, groupID, displayName string) (*Group, error) {
	if s == nil {
		return nil, fmt.Errorf("upsert group: service is not configured")
	}
	groupID = strings.TrimSpace(groupID)
	displayName = strings.TrimSpace(displayName)
	if groupID == "" {
		return nil, fmt.Errorf("upsert group: id is required")
	}
	if displayName == "" {
		return nil, fmt.Errorf("upsert group: display name is required")
	}
	now := time.Now()
	rec, err := s.store.Get(ctx, groupID)
	switch {
	case err == nil:
		rec["display_name"] = displayName
		rec["updated_at"] = now
		if err := s.store.Put(ctx, rec); err != nil {
			return nil, fmt.Errorf("upsert group: %w", err)
		}
		return recordToGroup(rec), nil
	case errors.Is(err, idb.ErrNotFound):
		created := newGroupRecord(groupID, displayName, now)
		if err := s.store.Add(ctx, created); err != nil {
			return nil, fmt.Errorf("upsert group: %w", err)
		}
		return recordToGroup(created), nil
	default:
		return nil, fmt.Errorf("upsert group: %w", err)
	}
}

func newGroupRecord(id, displayName string, now time.Time) idb.Record {
	return idb.Record{
		"id":           id,
		"display_name": displayName,
		"created_at":   now,
		"updated_at":   now,
	}
}

func recordToGroup(rec idb.Record) *Group {
	return &Group{
		ID:          recString(rec, "id"),
		DisplayName: recString(rec, "display_name"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
