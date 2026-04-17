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

type ServiceAccountDelegationService struct {
	store indexeddb.ObjectStore
}

func NewServiceAccountDelegationService(ds indexeddb.IndexedDB) *ServiceAccountDelegationService {
	return &ServiceAccountDelegationService{store: ds.ObjectStore(StoreServiceAccountDelegations)}
}

func (s *ServiceAccountDelegationService) UpsertDelegation(ctx context.Context, delegation *core.ServiceAccountDelegation) (*core.ServiceAccountDelegation, error) {
	if delegation == nil {
		return nil, fmt.Errorf("upsert service account delegation: delegation is required")
	}
	actorID := strings.TrimSpace(delegation.ActorUserPrincipalID)
	targetID := strings.TrimSpace(delegation.TargetServiceAccountPrincipalID)
	if actorID == "" || targetID == "" {
		return nil, fmt.Errorf("upsert service account delegation: actor_user_principal_id and target_service_account_principal_id are required")
	}
	if delegation.ID == "" {
		delegation.ID = newRecordID()
	}
	now := time.Now()
	if delegation.CreatedAt.IsZero() {
		delegation.CreatedAt = now
	}
	delegation.UpdatedAt = now
	rec := indexeddb.Record{
		"id":                                  delegation.ID,
		"actor_user_principal_id":             actorID,
		"target_service_account_principal_id": targetID,
		"plugin":                              strings.TrimSpace(delegation.Plugin),
		"expires_at":                          delegation.ExpiresAt,
		"created_at":                          delegation.CreatedAt,
		"updated_at":                          delegation.UpdatedAt,
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("upsert service account delegation: %w", err)
	}
	return recordToServiceAccountDelegation(rec), nil
}

func recordToServiceAccountDelegation(rec indexeddb.Record) *core.ServiceAccountDelegation {
	return &core.ServiceAccountDelegation{
		ID:                              recString(rec, "id"),
		ActorUserPrincipalID:            recString(rec, "actor_user_principal_id"),
		TargetServiceAccountPrincipalID: recString(rec, "target_service_account_principal_id"),
		Plugin:                          recString(rec, "plugin"),
		ExpiresAt:                       recTimePtr(rec, "expires_at"),
		CreatedAt:                       recTime(rec, "created_at"),
		UpdatedAt:                       recTime(rec, "updated_at"),
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
	principalID := strings.TrimSpace(credential.PrincipalID)
	plugin := strings.TrimSpace(credential.Plugin)
	connection := strings.TrimSpace(credential.Connection)
	instance := strings.TrimSpace(credential.Instance)
	authType := strings.TrimSpace(credential.AuthType)
	if principalID == "" || plugin == "" || connection == "" || authType == "" {
		return nil, fmt.Errorf("upsert external credential: principal_id, plugin, connection, and auth_type are required")
	}

	now := time.Now()
	id := credential.ID
	createdAt := credential.CreatedAt
	if existing, err := s.store.Index("by_lookup").Get(ctx, principalID, plugin, connection, instance); err == nil {
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
		"principal_id":        principalID,
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

func (s *ExternalCredentialService) DeleteCredential(ctx context.Context, principalID, plugin, connection, instance string) error {
	deleted, err := s.store.Index("by_lookup").Delete(ctx, strings.TrimSpace(principalID), strings.TrimSpace(plugin), strings.TrimSpace(connection), strings.TrimSpace(instance))
	if err != nil {
		return fmt.Errorf("delete external credential: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *ExternalCredentialService) GetCredential(ctx context.Context, principalID, plugin, connection, instance string) (*core.ExternalCredential, error) {
	rec, err := s.store.Index("by_lookup").Get(ctx, strings.TrimSpace(principalID), strings.TrimSpace(plugin), strings.TrimSpace(connection), strings.TrimSpace(instance))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get external credential: %w", err)
	}
	return recordToExternalCredential(rec), nil
}

type ServiceAccountAuthBindingService struct {
	store indexeddb.ObjectStore
}

func NewServiceAccountAuthBindingService(ds indexeddb.IndexedDB) *ServiceAccountAuthBindingService {
	return &ServiceAccountAuthBindingService{store: ds.ObjectStore(StoreServiceAccountAuthBindings)}
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
		PrincipalID:       recString(rec, "principal_id"),
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
