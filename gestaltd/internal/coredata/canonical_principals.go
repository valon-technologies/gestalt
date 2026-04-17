package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const identityStatusActive = "active"

const legacyIdentityBindingAuthority = "legacy"

type IdentityService struct {
	store indexeddb.ObjectStore
}

func NewIdentityService(ds indexeddb.IndexedDB) *IdentityService {
	return &IdentityService{store: ds.ObjectStore(StoreIdentities)}
}

func (s *IdentityService) UpsertIdentity(ctx context.Context, identity *core.Identity) (*core.Identity, error) {
	if identity == nil {
		return nil, fmt.Errorf("upsert identity: identity is required")
	}
	id := strings.TrimSpace(identity.ID)
	displayName := strings.TrimSpace(identity.DisplayName)
	if id == "" || displayName == "" {
		return nil, fmt.Errorf("upsert identity: id and display_name are required")
	}
	status := strings.TrimSpace(identity.Status)
	if status == "" {
		status = identityStatusActive
	}

	now := time.Now()
	createdAt := identity.CreatedAt
	if existing, err := s.store.Get(ctx, id); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert identity: %w", err)
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":                     id,
		"status":                 status,
		"display_name":           displayName,
		"created_by_identity_id": strings.TrimSpace(identity.CreatedByIdentityID),
		"metadata_json":          strings.TrimSpace(identity.MetadataJSON),
		"created_at":             createdAt,
		"updated_at":             now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert identity: %w", err)
	}
	return recordToIdentity(rec), nil
}

func (s *IdentityService) GetIdentity(ctx context.Context, id string) (*core.Identity, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get identity: %w", err)
	}
	return recordToIdentity(rec), nil
}

func (s *IdentityService) DeleteIdentity(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, strings.TrimSpace(id)); err != nil {
		if err == indexeddb.ErrNotFound {
			return core.ErrNotFound
		}
		return fmt.Errorf("delete identity: %w", err)
	}
	return nil
}

func (s *IdentityService) ListIdentities(ctx context.Context) ([]*core.Identity, error) {
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	out := make([]*core.Identity, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToIdentity(rec))
	}
	return out, nil
}

func recordToIdentity(rec indexeddb.Record) *core.Identity {
	return &core.Identity{
		ID:                  recString(rec, "id"),
		Status:              recString(rec, "status"),
		DisplayName:         recString(rec, "display_name"),
		CreatedByIdentityID: recString(rec, "created_by_identity_id"),
		MetadataJSON:        recString(rec, "metadata_json"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
}

type IdentityAuthBindingService struct {
	store indexeddb.ObjectStore
}

func NewIdentityAuthBindingService(ds indexeddb.IndexedDB) *IdentityAuthBindingService {
	return &IdentityAuthBindingService{store: ds.ObjectStore(StoreIdentityAuthBindings)}
}

func (s *IdentityAuthBindingService) UpsertBinding(ctx context.Context, binding *core.IdentityAuthBinding) (*core.IdentityAuthBinding, error) {
	if binding == nil {
		return nil, fmt.Errorf("upsert identity auth binding: binding is required")
	}
	identityID := strings.TrimSpace(binding.IdentityID)
	bindingKind := strings.TrimSpace(binding.BindingKind)
	authority := strings.TrimSpace(binding.Authority)
	lookupKey := strings.TrimSpace(binding.LookupKey)
	if identityID == "" || bindingKind == "" || authority == "" || lookupKey == "" {
		return nil, fmt.Errorf("upsert identity auth binding: identity_id, binding_kind, authority, and lookup_key are required")
	}

	now := time.Now()
	id := binding.ID
	createdAt := binding.CreatedAt
	if existing, err := s.store.Index("by_lookup").Get(ctx, bindingKind, authority, lookupKey); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert identity auth binding: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":           id,
		"identity_id":  identityID,
		"binding_kind": bindingKind,
		"authority":    authority,
		"lookup_key":   lookupKey,
		"binding_json": strings.TrimSpace(binding.BindingJSON),
		"created_at":   createdAt,
		"updated_at":   now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert identity auth binding: %w", err)
	}
	return recordToIdentityAuthBinding(rec), nil
}

func (s *IdentityAuthBindingService) GetBinding(ctx context.Context, bindingKind, authority, lookupKey string) (*core.IdentityAuthBinding, error) {
	rec, err := s.store.Index("by_lookup").Get(ctx, strings.TrimSpace(bindingKind), strings.TrimSpace(authority), strings.TrimSpace(lookupKey))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get identity auth binding: %w", err)
	}
	return recordToIdentityAuthBinding(rec), nil
}

func (s *IdentityAuthBindingService) ListByIdentity(ctx context.Context, identityID string) ([]*core.IdentityAuthBinding, error) {
	recs, err := s.store.Index("by_identity").GetAll(ctx, nil, strings.TrimSpace(identityID))
	if err != nil {
		return nil, fmt.Errorf("list identity auth bindings: %w", err)
	}
	out := make([]*core.IdentityAuthBinding, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToIdentityAuthBinding(rec))
	}
	return out, nil
}

func (s *IdentityAuthBindingService) DeleteBinding(ctx context.Context, bindingKind, authority, lookupKey string) error {
	deleted, err := s.store.Index("by_lookup").Delete(ctx, strings.TrimSpace(bindingKind), strings.TrimSpace(authority), strings.TrimSpace(lookupKey))
	if err != nil {
		return fmt.Errorf("delete identity auth binding: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func recordToIdentityAuthBinding(rec indexeddb.Record) *core.IdentityAuthBinding {
	return &core.IdentityAuthBinding{
		ID:          recString(rec, "id"),
		IdentityID:  recString(rec, "identity_id"),
		BindingKind: recString(rec, "binding_kind"),
		Authority:   recString(rec, "authority"),
		LookupKey:   recString(rec, "lookup_key"),
		BindingJSON: recString(rec, "binding_json"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}

func newRecordID() string {
	return uuid.NewString()
}

func legacyIdentityMetadataJSON(label string, extra map[string]string) string {
	payload := map[string]string{
		"label": label,
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(raw)
}
