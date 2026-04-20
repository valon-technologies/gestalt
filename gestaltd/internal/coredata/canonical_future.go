package coredata

import (
	"context"
	"encoding/json"
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

type ExternalCredentialService struct {
	store indexeddb.ObjectStore
}

func NewExternalCredentialService(ds indexeddb.IndexedDB) *ExternalCredentialService {
	return &ExternalCredentialService{store: ds.ObjectStore(StoreExternalCredentials)}
}

func (s *ExternalCredentialService) UpsertCredential(ctx context.Context, credential *core.ExternalCredential) (*core.ExternalCredential, error) {
	if credential == nil {
		return nil, fmt.Errorf("upsert external credential: credential is required")
	}
	identityID := strings.TrimSpace(credential.IdentityID)
	plugin := strings.TrimSpace(credential.Plugin)
	connection := strings.TrimSpace(credential.Connection)
	instance := strings.TrimSpace(credential.Instance)
	authType := strings.TrimSpace(credential.AuthType)
	if identityID == "" || plugin == "" || connection == "" || authType == "" {
		return nil, fmt.Errorf("upsert external credential: identity_id, plugin, connection, and auth_type are required")
	}

	now := time.Now()
	id := credential.ID
	createdAt := credential.CreatedAt
	if existing, err := s.store.Index("by_lookup").Get(ctx, identityID, plugin, connection, instance); err == nil {
		id = recString(existing, "id")
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("upsert external credential: %w", err)
	}
	if id == "" {
		id = newRecordID()
	}
	if createdAt.IsZero() {
		createdAt = now
	}

	rec := indexeddb.Record{
		"id":                  id,
		"identity_id":         identityID,
		"plugin":              plugin,
		"connection":          connection,
		"instance":            instance,
		"auth_type":           authType,
		"payload_encrypted":   credential.PayloadEncrypted,
		"scopes":              credential.Scopes,
		"expires_at":          credential.ExpiresAt,
		"last_refreshed_at":   credential.LastRefreshedAt,
		"refresh_error_count": credential.RefreshErrorCount,
		"metadata_json":       credential.MetadataJSON,
		"created_at":          createdAt,
		"updated_at":          now,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert external credential: %w", err)
	}
	return recordToExternalCredential(rec), nil
}

func (s *ExternalCredentialService) DeleteCredential(ctx context.Context, identityID, plugin, connection, instance string) error {
	deleted, err := s.store.Index("by_lookup").Delete(ctx, strings.TrimSpace(identityID), strings.TrimSpace(plugin), strings.TrimSpace(connection), strings.TrimSpace(instance))
	if err != nil {
		return fmt.Errorf("delete external credential: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *ExternalCredentialService) GetCredential(ctx context.Context, identityID, plugin, connection, instance string) (*core.ExternalCredential, error) {
	rec, err := s.store.Index("by_lookup").Get(ctx, strings.TrimSpace(identityID), strings.TrimSpace(plugin), strings.TrimSpace(connection), strings.TrimSpace(instance))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get external credential: %w", err)
	}
	return recordToExternalCredential(rec), nil
}

func (s *ExternalCredentialService) ListByIdentityConnection(ctx context.Context, identityID, plugin, connection string) ([]*core.ExternalCredential, error) {
	recs, err := s.store.Index("by_identity_connection").GetAll(ctx, nil, strings.TrimSpace(identityID), strings.TrimSpace(plugin), strings.TrimSpace(connection))
	if err != nil {
		return nil, fmt.Errorf("list external credentials: %w", err)
	}
	out := make([]*core.ExternalCredential, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToExternalCredential(rec))
	}
	return out, nil
}

func encodeLegacyCredentialPayload(accessTokenEncrypted, refreshTokenEncrypted string) (string, error) {
	payload := map[string]string{
		"access_token_encrypted":  accessTokenEncrypted,
		"refresh_token_encrypted": refreshTokenEncrypted,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode legacy credential payload: %w", err)
	}
	return string(raw), nil
}

func recordToExternalCredential(rec indexeddb.Record) *core.ExternalCredential {
	return &core.ExternalCredential{
		ID:                recString(rec, "id"),
		IdentityID:        recString(rec, "identity_id"),
		Plugin:            recString(rec, "plugin"),
		Connection:        recString(rec, "connection"),
		Instance:          recString(rec, "instance"),
		AuthType:          recString(rec, "auth_type"),
		PayloadEncrypted:  recString(rec, "payload_encrypted"),
		Scopes:            recString(rec, "scopes"),
		ExpiresAt:         recTimePtr(rec, "expires_at"),
		LastRefreshedAt:   recTimePtr(rec, "last_refreshed_at"),
		RefreshErrorCount: recInt(rec, "refresh_error_count"),
		MetadataJSON:      recString(rec, "metadata_json"),
		CreatedAt:         recTime(rec, "created_at"),
		UpdatedAt:         recTime(rec, "updated_at"),
	}
}
