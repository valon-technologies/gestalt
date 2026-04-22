package coredata

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type IdentityDelegationService struct {
	store indexeddb.ObjectStore
}

func NewIdentityDelegationService(ds indexeddb.IndexedDB) *IdentityDelegationService {
	return &IdentityDelegationService{store: ds.ObjectStore(StoreIdentityDelegations)}
}

func (s *IdentityDelegationService) UpsertDelegation(ctx context.Context, delegation *core.IdentityDelegation) (*core.IdentityDelegation, error) {
	if delegation == nil {
		return nil, fmt.Errorf("upsert identity delegation: delegation is required")
	}
	actorID := strings.TrimSpace(delegation.ActorIdentityID)
	targetID := strings.TrimSpace(delegation.TargetIdentityID)
	if actorID == "" || targetID == "" {
		return nil, fmt.Errorf("upsert identity delegation: actor_identity_id and target_identity_id are required")
	}

	now := time.Now()
	id := delegation.ID
	createdAt := delegation.CreatedAt
	plugin := strings.TrimSpace(delegation.Plugin)
	if existing, err := s.store.Index("by_actor_target_plugin").Get(ctx, actorID, targetID, plugin); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert identity delegation: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":                 id,
		"actor_identity_id":  actorID,
		"target_identity_id": targetID,
		"plugin":             plugin,
		"expires_at":         delegation.ExpiresAt,
		"created_at":         createdAt,
		"updated_at":         now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert identity delegation: %w", err)
	}
	return recordToIdentityDelegation(rec), nil
}

func (s *IdentityDelegationService) GetDelegation(ctx context.Context, actorIdentityID, targetIdentityID, plugin string) (*core.IdentityDelegation, error) {
	rec, err := s.store.Index("by_actor_target_plugin").Get(ctx, strings.TrimSpace(actorIdentityID), strings.TrimSpace(targetIdentityID), strings.TrimSpace(plugin))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get identity delegation: %w", err)
	}
	return recordToIdentityDelegation(rec), nil
}

func (s *IdentityDelegationService) DeleteDelegation(ctx context.Context, actorIdentityID, targetIdentityID, plugin string) error {
	deleted, err := s.store.Index("by_actor_target_plugin").Delete(ctx, strings.TrimSpace(actorIdentityID), strings.TrimSpace(targetIdentityID), strings.TrimSpace(plugin))
	if err != nil {
		return fmt.Errorf("delete identity delegation: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func recordToIdentityDelegation(rec indexeddb.Record) *core.IdentityDelegation {
	return &core.IdentityDelegation{
		ID:               recString(rec, "id"),
		ActorIdentityID:  recString(rec, "actor_identity_id"),
		TargetIdentityID: recString(rec, "target_identity_id"),
		Plugin:           recString(rec, "plugin"),
		ExpiresAt:        recTimePtr(rec, "expires_at"),
		CreatedAt:        recTime(rec, "created_at"),
		UpdatedAt:        recTime(rec, "updated_at"),
	}
}
