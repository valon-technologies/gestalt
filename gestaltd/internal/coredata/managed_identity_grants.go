package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type ManagedIdentityGrantService struct {
	store           indexeddb.ObjectStore
	canonicalAccess *IdentityPluginAccessService
}

var errMalformedStoredManagedIdentityGrant = errors.New("malformed stored managed identity grant")

func NewManagedIdentityGrantService(ds indexeddb.IndexedDB, canonicalAccess *IdentityPluginAccessService) *ManagedIdentityGrantService {
	return &ManagedIdentityGrantService{
		store:           ds.ObjectStore(StoreManagedIdentityGrants),
		canonicalAccess: canonicalAccess,
	}
}

func (s *ManagedIdentityGrantService) UpsertGrant(ctx context.Context, grant *core.ManagedIdentityGrant) (*core.ManagedIdentityGrant, error) {
	if grant == nil || grant.IdentityID == "" || grant.Plugin == "" {
		return nil, fmt.Errorf("upsert managed identity grant: identity_id and plugin are required")
	}

	now := time.Now()
	rec, err := s.store.Index("by_identity_plugin").Get(ctx, grant.IdentityID, grant.Plugin)
	switch {
	case err == nil:
		existing, err := recordToManagedIdentityGrant(rec)
		if err != nil {
			return nil, fmt.Errorf("update managed identity grant: %w", err)
		}
		existing.Operations = append([]string(nil), grant.Operations...)
		existing.UpdatedAt = now
		record, err := managedIdentityGrantRecord(existing)
		if err != nil {
			return nil, fmt.Errorf("update managed identity grant: %w", err)
		}
		if err := s.store.Put(ctx, record); err != nil {
			return nil, fmt.Errorf("update managed identity grant: %w", err)
		}
		if err := s.syncCanonicalAccess(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	case err != nil && err != indexeddb.ErrNotFound:
		return nil, fmt.Errorf("lookup managed identity grant: %w", err)
	}

	if grant.ID == "" {
		grant.ID = uuid.NewString()
	}
	grant.CreatedAt = now
	grant.UpdatedAt = now
	record, err := managedIdentityGrantRecord(grant)
	if err != nil {
		return nil, fmt.Errorf("create managed identity grant: %w", err)
	}
	if err := s.store.Add(ctx, record); err != nil {
		return nil, fmt.Errorf("create managed identity grant: %w", err)
	}
	if err := s.syncCanonicalAccess(ctx, grant); err != nil {
		return nil, err
	}
	return grant, nil
}

func (s *ManagedIdentityGrantService) GetGrant(ctx context.Context, identityID, plugin string) (*core.ManagedIdentityGrant, error) {
	rec, err := s.store.Index("by_identity_plugin").Get(ctx, identityID, plugin)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get managed identity grant: %w", err)
	}
	grant, err := recordToManagedIdentityGrant(rec)
	if err != nil {
		return nil, fmt.Errorf("get managed identity grant: %w", err)
	}
	return grant, nil
}

func (s *ManagedIdentityGrantService) ListGrantsByIdentity(ctx context.Context, identityID string) ([]*core.ManagedIdentityGrant, error) {
	recs, err := s.store.Index("by_identity").GetAll(ctx, nil, identityID)
	if err != nil {
		return nil, fmt.Errorf("list managed identity grants: %w", err)
	}
	out := make([]*core.ManagedIdentityGrant, 0, len(recs))
	for _, rec := range recs {
		grant, err := recordToManagedIdentityGrant(rec)
		if err != nil {
			return nil, fmt.Errorf("list managed identity grants: %w", err)
		}
		out = append(out, grant)
	}
	return out, nil
}

func (s *ManagedIdentityGrantService) DeleteGrant(ctx context.Context, identityID, plugin string) error {
	deleted, err := s.store.Index("by_identity_plugin").Delete(ctx, identityID, plugin)
	if err != nil {
		return fmt.Errorf("delete managed identity grant: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	if s.canonicalAccess != nil {
		if err := s.canonicalAccess.DeleteAccess(ctx, identityID, plugin); err != nil && err != core.ErrNotFound {
			return fmt.Errorf("delete canonical identity plugin access: %w", err)
		}
	}
	return nil
}

func (s *ManagedIdentityGrantService) RestoreGrant(ctx context.Context, grant *core.ManagedIdentityGrant) error {
	if grant == nil || grant.ID == "" || grant.IdentityID == "" || grant.Plugin == "" {
		return fmt.Errorf("restore managed identity grant: id, identity_id, and plugin are required")
	}
	record, err := managedIdentityGrantRecord(grant)
	if err != nil {
		return fmt.Errorf("restore managed identity grant: %w", err)
	}
	if err := s.store.Put(ctx, record); err != nil {
		return fmt.Errorf("restore managed identity grant: %w", err)
	}
	if err := s.syncCanonicalAccess(ctx, grant); err != nil {
		return err
	}
	return nil
}

func (s *ManagedIdentityGrantService) BackfillCanonicalAccess(ctx context.Context) error {
	if s.canonicalAccess == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list managed identity grants for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		grant, err := recordToManagedIdentityGrant(rec)
		if err != nil {
			if errors.Is(err, errMalformedStoredManagedIdentityGrant) {
				continue
			}
			return err
		}
		if err := s.syncCanonicalAccess(ctx, grant); err != nil {
			return err
		}
	}
	return nil
}

func managedIdentityGrantRecord(grant *core.ManagedIdentityGrant) (indexeddb.Record, error) {
	operationsJSON := ""
	if len(grant.Operations) > 0 {
		b, err := json.Marshal(grant.Operations)
		if err != nil {
			return nil, fmt.Errorf("marshal managed identity grant operations: %w", err)
		}
		operationsJSON = string(b)
	}
	return indexeddb.Record{
		"id":              grant.ID,
		"identity_id":     grant.IdentityID,
		"plugin":          grant.Plugin,
		"operations_json": operationsJSON,
		"created_at":      grant.CreatedAt,
		"updated_at":      grant.UpdatedAt,
	}, nil
}

func recordToManagedIdentityGrant(rec indexeddb.Record) (*core.ManagedIdentityGrant, error) {
	grant := &core.ManagedIdentityGrant{
		ID:         recString(rec, "id"),
		IdentityID: recString(rec, "identity_id"),
		Plugin:     recString(rec, "plugin"),
		CreatedAt:  recTime(rec, "created_at"),
		UpdatedAt:  recTime(rec, "updated_at"),
	}
	if raw := recString(rec, "operations_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &grant.Operations); err != nil {
			return nil, fmt.Errorf("%w: decode managed identity grant operations: %v", errMalformedStoredManagedIdentityGrant, err)
		}
	}
	return grant, nil
}

func (s *ManagedIdentityGrantService) syncCanonicalAccess(ctx context.Context, grant *core.ManagedIdentityGrant) error {
	if s.canonicalAccess == nil || grant == nil || grant.IdentityID == "" || grant.Plugin == "" {
		return nil
	}
	if _, err := s.canonicalAccess.UpsertAccess(ctx, &core.IdentityPluginAccess{
		IdentityID:          grant.IdentityID,
		Plugin:              grant.Plugin,
		InvokeAllOperations: len(grant.Operations) == 0,
		Operations:          append([]string(nil), grant.Operations...),
		CreatedAt:           grant.CreatedAt,
		UpdatedAt:           grant.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical identity plugin access %q/%q: %w", grant.IdentityID, grant.Plugin, err)
	}
	return nil
}
