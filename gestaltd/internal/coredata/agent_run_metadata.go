package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AgentRunMetadataService struct {
	store            indexeddb.ObjectStore
	idempotencyStore indexeddb.ObjectStore
}

func NewAgentRunMetadataService(ds indexeddb.IndexedDB) *AgentRunMetadataService {
	return &AgentRunMetadataService{
		store:            ds.ObjectStore(StoreAgentRunMetadata),
		idempotencyStore: ds.ObjectStore(StoreAgentRunIdempotency),
	}
}

func (s *AgentRunMetadataService) Put(ctx context.Context, ref *coreagent.ExecutionReference) (*coreagent.ExecutionReference, error) {
	if ref == nil {
		return nil, fmt.Errorf("put agent run metadata: ref is required")
	}

	id := strings.TrimSpace(ref.ID)
	providerName := strings.TrimSpace(ref.ProviderName)
	subjectID := strings.TrimSpace(ref.SubjectID)
	if id == "" || providerName == "" || subjectID == "" {
		return nil, fmt.Errorf("put agent run metadata: id, provider_name, and subject_id are required")
	}

	permissionsJSON, err := marshalJSON(ref.Permissions)
	if err != nil {
		return nil, fmt.Errorf("put agent run metadata: marshal permissions: %w", err)
	}
	toolsJSON, err := marshalJSON(ref.Tools)
	if err != nil {
		return nil, fmt.Errorf("put agent run metadata: marshal tools: %w", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	createdAt := ref.CreatedAt
	if existing, err := s.store.Get(ctx, id); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = &created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("put agent run metadata: %w", err)
	}
	if createdAt == nil || createdAt.IsZero() {
		createdAt = &now
	}

	rec := indexeddb.Record{
		"id":                    id,
		"provider_name":         providerName,
		"subject_id":            subjectID,
		"credential_subject_id": strings.TrimSpace(ref.CredentialSubjectID),
		"permissions_json":      permissionsJSON,
		"idempotency_key":       strings.TrimSpace(ref.IdempotencyKey),
		"tools_json":            toolsJSON,
		"created_at":            *createdAt,
		"revoked_at":            timeOrNil(ref.RevokedAt),
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("put agent run metadata: %w", err)
	}
	if err := s.putIdempotencyRecord(ctx, recordToAgentRunMetadata(rec)); err != nil {
		return nil, err
	}
	return recordToAgentRunMetadata(rec), nil
}

func (s *AgentRunMetadataService) Get(ctx context.Context, id string) (*coreagent.ExecutionReference, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, indexeddb.ErrNotFound
		}
		return nil, fmt.Errorf("get agent run metadata: %w", err)
	}
	return recordToAgentRunMetadata(rec), nil
}

func (s *AgentRunMetadataService) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	ref, err := s.Get(ctx, id)
	if err != nil && err != indexeddb.ErrNotFound {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("delete agent run metadata: %w", err)
	}
	if ref != nil {
		if err := s.deleteIdempotencyRecord(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentRunMetadataService) ListBySubject(ctx context.Context, subjectID string) ([]*coreagent.ExecutionReference, error) {
	recs, err := s.store.Index("by_subject").GetAll(ctx, nil, strings.TrimSpace(subjectID))
	if err != nil {
		return nil, fmt.Errorf("list agent run metadata by subject: %w", err)
	}
	out := make([]*coreagent.ExecutionReference, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAgentRunMetadata(rec))
	}
	return out, nil
}

func (s *AgentRunMetadataService) GetByIdempotency(ctx context.Context, subjectID, providerName, idempotencyKey string) (*coreagent.ExecutionReference, error) {
	recordID := agentRunIdempotencyRecordID(subjectID, providerName, idempotencyKey)
	if recordID == "" {
		return nil, indexeddb.ErrNotFound
	}
	rec, err := s.idempotencyStore.Get(ctx, recordID)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, indexeddb.ErrNotFound
		}
		return nil, fmt.Errorf("get agent run idempotency: %w", err)
	}
	runID := recString(rec, "run_id")
	if strings.TrimSpace(runID) == "" {
		return nil, indexeddb.ErrNotFound
	}
	return s.Get(ctx, runID)
}

func (s *AgentRunMetadataService) ClaimIdempotency(ctx context.Context, subjectID, providerName, idempotencyKey, runID string, createdAt time.Time) (string, bool, error) {
	recordID := agentRunIdempotencyRecordID(subjectID, providerName, idempotencyKey)
	if recordID == "" {
		return "", false, indexeddb.ErrNotFound
	}
	rec := indexeddb.Record{
		"id":              recordID,
		"run_id":          strings.TrimSpace(runID),
		"subject_id":      strings.TrimSpace(subjectID),
		"provider_name":   strings.TrimSpace(providerName),
		"idempotency_key": strings.TrimSpace(idempotencyKey),
		"created_at":      createdAt.UTC().Truncate(time.Second),
	}
	if err := s.idempotencyStore.Add(ctx, rec); err == nil {
		return strings.TrimSpace(runID), true, nil
	} else if !errors.Is(err, indexeddb.ErrAlreadyExists) {
		return "", false, fmt.Errorf("claim agent run idempotency: %w", err)
	}

	existing, err := s.idempotencyStore.Get(ctx, recordID)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return "", false, indexeddb.ErrNotFound
		}
		return "", false, fmt.Errorf("claim agent run idempotency: %w", err)
	}
	return recString(existing, "run_id"), false, nil
}

func (s *AgentRunMetadataService) ReleaseIdempotency(ctx context.Context, subjectID, providerName, idempotencyKey string) error {
	recordID := agentRunIdempotencyRecordID(subjectID, providerName, idempotencyKey)
	if recordID == "" {
		return nil
	}
	if err := s.idempotencyStore.Delete(ctx, recordID); err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("release agent run idempotency: %w", err)
	}
	return nil
}

func (s *AgentRunMetadataService) putIdempotencyRecord(ctx context.Context, ref *coreagent.ExecutionReference) error {
	if s == nil || s.idempotencyStore == nil || ref == nil {
		return nil
	}
	recordID := agentRunIdempotencyRecordID(ref.SubjectID, ref.ProviderName, ref.IdempotencyKey)
	if recordID == "" {
		return nil
	}
	rec := indexeddb.Record{
		"id":              recordID,
		"run_id":          strings.TrimSpace(ref.ID),
		"subject_id":      strings.TrimSpace(ref.SubjectID),
		"provider_name":   strings.TrimSpace(ref.ProviderName),
		"idempotency_key": strings.TrimSpace(ref.IdempotencyKey),
		"created_at":      timeOrNil(ref.CreatedAt),
	}
	if err := s.idempotencyStore.Put(ctx, rec); err != nil {
		return fmt.Errorf("put agent run idempotency: %w", err)
	}
	return nil
}

func (s *AgentRunMetadataService) deleteIdempotencyRecord(ctx context.Context, ref *coreagent.ExecutionReference) error {
	if s == nil || s.idempotencyStore == nil || ref == nil {
		return nil
	}
	recordID := agentRunIdempotencyRecordID(ref.SubjectID, ref.ProviderName, ref.IdempotencyKey)
	if recordID == "" {
		return nil
	}
	if err := s.idempotencyStore.Delete(ctx, recordID); err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("delete agent run idempotency: %w", err)
	}
	return nil
}

func recordToAgentRunMetadata(rec indexeddb.Record) *coreagent.ExecutionReference {
	return &coreagent.ExecutionReference{
		ID:                  recString(rec, "id"),
		ProviderName:        recString(rec, "provider_name"),
		SubjectID:           recString(rec, "subject_id"),
		CredentialSubjectID: recString(rec, "credential_subject_id"),
		IdempotencyKey:      recString(rec, "idempotency_key"),
		Permissions:         recAgentRunMetadataPermissions(rec),
		Tools:               recAgentRunMetadataTools(rec),
		CreatedAt:           recTimePtr(rec, "created_at"),
		RevokedAt:           recTimePtr(rec, "revoked_at"),
	}
}

func recAgentRunMetadataPermissions(rec indexeddb.Record) []core.AccessPermission {
	raw := recString(rec, "permissions_json")
	if raw == "" {
		return nil
	}
	var permissions []core.AccessPermission
	if err := json.Unmarshal([]byte(raw), &permissions); err != nil {
		return nil
	}
	return permissions
}

func recAgentRunMetadataTools(rec indexeddb.Record) []coreagent.Tool {
	raw := recString(rec, "tools_json")
	if raw == "" {
		return nil
	}
	var tools []coreagent.Tool
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		return nil
	}
	return tools
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "", nil
	}
	return string(raw), nil
}

func agentRunIdempotencyRecordID(subjectID, providerName, idempotencyKey string) string {
	subjectID = strings.TrimSpace(subjectID)
	providerName = strings.TrimSpace(providerName)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if subjectID == "" || providerName == "" || idempotencyKey == "" {
		return ""
	}
	return subjectID + "\x00" + providerName + "\x00" + idempotencyKey
}
