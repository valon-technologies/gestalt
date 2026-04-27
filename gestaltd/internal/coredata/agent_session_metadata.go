package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AgentSessionMetadataService struct {
	store            indexeddb.ObjectStore
	idempotencyStore indexeddb.ObjectStore
}

func NewAgentSessionMetadataService(ds indexeddb.IndexedDB) *AgentSessionMetadataService {
	return &AgentSessionMetadataService{
		store:            ds.ObjectStore(StoreAgentSessionMetadata),
		idempotencyStore: ds.ObjectStore(StoreAgentSessionIdempotency),
	}
}

func (s *AgentSessionMetadataService) Put(ctx context.Context, ref *coreagent.SessionReference) (*coreagent.SessionReference, error) {
	if ref == nil {
		return nil, fmt.Errorf("put agent session metadata: ref is required")
	}
	id := strings.TrimSpace(ref.ID)
	providerName := strings.TrimSpace(ref.ProviderName)
	subjectID := strings.TrimSpace(ref.SubjectID)
	if id == "" || providerName == "" || subjectID == "" {
		return nil, fmt.Errorf("put agent session metadata: id, provider_name, and subject_id are required")
	}
	now := time.Now().UTC().Truncate(time.Second)
	createdAt := ref.CreatedAt
	if existing, err := s.store.Get(ctx, id); err == nil {
		if created := recTime(existing, "created_at"); !created.IsZero() {
			createdAt = &created
		}
	} else if err != indexeddb.ErrNotFound {
		return nil, fmt.Errorf("put agent session metadata: %w", err)
	}
	if createdAt == nil || createdAt.IsZero() {
		createdAt = &now
	}
	rec := indexeddb.Record{
		"id":                    id,
		"provider_name":         providerName,
		"subject_id":            subjectID,
		"credential_subject_id": strings.TrimSpace(ref.CredentialSubjectID),
		"idempotency_key":       strings.TrimSpace(ref.IdempotencyKey),
		"created_at":            *createdAt,
		"archived_at":           timeOrNil(ref.ArchivedAt),
	}
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("put agent session metadata: %w", err)
	}
	if err := s.putIdempotencyRecord(ctx, recordToAgentSessionMetadata(rec)); err != nil {
		return nil, err
	}
	return recordToAgentSessionMetadata(rec), nil
}

func (s *AgentSessionMetadataService) Get(ctx context.Context, id string) (*coreagent.SessionReference, error) {
	rec, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, indexeddb.ErrNotFound
		}
		return nil, fmt.Errorf("get agent session metadata: %w", err)
	}
	return recordToAgentSessionMetadata(rec), nil
}

func (s *AgentSessionMetadataService) ListBySubject(ctx context.Context, subjectID string) ([]*coreagent.SessionReference, error) {
	recs, err := s.store.Index("by_subject").GetAll(ctx, nil, strings.TrimSpace(subjectID))
	if err != nil {
		return nil, fmt.Errorf("list agent session metadata by subject: %w", err)
	}
	out := make([]*coreagent.SessionReference, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAgentSessionMetadata(rec))
	}
	return out, nil
}

func (s *AgentSessionMetadataService) Archive(ctx context.Context, id string, archivedAt time.Time) (*coreagent.SessionReference, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, indexeddb.ErrNotFound
	}
	ref, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if ref.ArchivedAt != nil && !ref.ArchivedAt.IsZero() {
		return ref, nil
	}
	if archivedAt.IsZero() {
		archivedAt = time.Now()
	}
	archivedAt = archivedAt.UTC().Truncate(time.Second)
	ref.ArchivedAt = &archivedAt
	return s.Put(ctx, ref)
}

func (s *AgentSessionMetadataService) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	ref, err := s.Get(ctx, id)
	if err != nil && err != indexeddb.ErrNotFound {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("delete agent session metadata: %w", err)
	}
	if ref != nil {
		if err := s.deleteIdempotencyRecord(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}

func (s *AgentSessionMetadataService) ClaimIdempotency(ctx context.Context, subjectID, providerName, idempotencyKey, sessionID string, createdAt time.Time) (string, bool, error) {
	recordID := agentSessionIdempotencyRecordID(subjectID, providerName, idempotencyKey)
	if recordID == "" {
		return "", false, indexeddb.ErrNotFound
	}
	rec := indexeddb.Record{
		"id":              recordID,
		"session_id":      strings.TrimSpace(sessionID),
		"subject_id":      strings.TrimSpace(subjectID),
		"provider_name":   strings.TrimSpace(providerName),
		"idempotency_key": strings.TrimSpace(idempotencyKey),
		"created_at":      createdAt.UTC().Truncate(time.Second),
	}
	if err := s.idempotencyStore.Add(ctx, rec); err == nil {
		return strings.TrimSpace(sessionID), true, nil
	} else if !errors.Is(err, indexeddb.ErrAlreadyExists) {
		return "", false, fmt.Errorf("claim agent session idempotency: %w", err)
	}
	existing, err := s.idempotencyStore.Get(ctx, recordID)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return "", false, indexeddb.ErrNotFound
		}
		return "", false, fmt.Errorf("claim agent session idempotency: %w", err)
	}
	return recString(existing, "session_id"), false, nil
}

func (s *AgentSessionMetadataService) ReleaseIdempotency(ctx context.Context, subjectID, providerName, idempotencyKey string) error {
	recordID := agentSessionIdempotencyRecordID(subjectID, providerName, idempotencyKey)
	if recordID == "" {
		return nil
	}
	if err := s.idempotencyStore.Delete(ctx, recordID); err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("release agent session idempotency: %w", err)
	}
	return nil
}

func (s *AgentSessionMetadataService) SessionIDForIdempotency(ctx context.Context, subjectID, providerName, idempotencyKey string) (string, error) {
	recordID := agentSessionIdempotencyRecordID(subjectID, providerName, idempotencyKey)
	if recordID == "" {
		return "", indexeddb.ErrNotFound
	}
	rec, err := s.idempotencyStore.Get(ctx, recordID)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return "", indexeddb.ErrNotFound
		}
		return "", fmt.Errorf("get agent session idempotency: %w", err)
	}
	return recString(rec, "session_id"), nil
}

func (s *AgentSessionMetadataService) putIdempotencyRecord(ctx context.Context, ref *coreagent.SessionReference) error {
	if s == nil || s.idempotencyStore == nil || ref == nil {
		return nil
	}
	recordID := agentSessionIdempotencyRecordID(ref.SubjectID, ref.ProviderName, ref.IdempotencyKey)
	if recordID == "" {
		return nil
	}
	rec := indexeddb.Record{
		"id":              recordID,
		"session_id":      strings.TrimSpace(ref.ID),
		"subject_id":      strings.TrimSpace(ref.SubjectID),
		"provider_name":   strings.TrimSpace(ref.ProviderName),
		"idempotency_key": strings.TrimSpace(ref.IdempotencyKey),
		"created_at":      timeOrNil(ref.CreatedAt),
	}
	if err := s.idempotencyStore.Put(ctx, rec); err != nil {
		return fmt.Errorf("put agent session idempotency: %w", err)
	}
	return nil
}

func (s *AgentSessionMetadataService) deleteIdempotencyRecord(ctx context.Context, ref *coreagent.SessionReference) error {
	if s == nil || s.idempotencyStore == nil || ref == nil {
		return nil
	}
	recordID := agentSessionIdempotencyRecordID(ref.SubjectID, ref.ProviderName, ref.IdempotencyKey)
	if recordID == "" {
		return nil
	}
	if err := s.idempotencyStore.Delete(ctx, recordID); err != nil && err != indexeddb.ErrNotFound {
		return fmt.Errorf("delete agent session idempotency: %w", err)
	}
	return nil
}

func recordToAgentSessionMetadata(rec indexeddb.Record) *coreagent.SessionReference {
	return &coreagent.SessionReference{
		ID:                  recString(rec, "id"),
		ProviderName:        recString(rec, "provider_name"),
		SubjectID:           recString(rec, "subject_id"),
		CredentialSubjectID: recString(rec, "credential_subject_id"),
		IdempotencyKey:      recString(rec, "idempotency_key"),
		CreatedAt:           recTimePtr(rec, "created_at"),
		ArchivedAt:          recTimePtr(rec, "archived_at"),
	}
}

func agentSessionIdempotencyRecordID(subjectID, providerName, idempotencyKey string) string {
	subjectID = strings.TrimSpace(subjectID)
	providerName = strings.TrimSpace(providerName)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if subjectID == "" || providerName == "" || idempotencyKey == "" {
		return ""
	}
	return subjectID + "::" + providerName + "::" + idempotencyKey
}
