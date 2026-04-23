package coredata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type LocalExternalCredentialProvider struct {
	store indexeddb.ObjectStore
	enc   *corecrypto.AESGCMEncryptor
}

type TokenService = LocalExternalCredentialProvider

var errUnreadableStoredIntegrationToken = errors.New("unreadable stored integration token")

var _ core.ExternalCredentialProvider = (*LocalExternalCredentialProvider)(nil)

func NewLocalExternalCredentialProvider(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) *LocalExternalCredentialProvider {
	return &LocalExternalCredentialProvider{
		store: ds.ObjectStore(StoreIntegrationTokens),
		enc:   enc,
	}
}

func NewTokenService(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) *LocalExternalCredentialProvider {
	return NewLocalExternalCredentialProvider(ds, enc)
}

func (s *LocalExternalCredentialProvider) PutCredential(ctx context.Context, credential *core.ExternalCredential) error {
	return s.storeToken(ctx, credential, false)
}

func (s *LocalExternalCredentialProvider) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) error {
	return s.storeToken(ctx, credential, true)
}

func (s *LocalExternalCredentialProvider) GetCredential(ctx context.Context, subjectID, integration, connection, instance string) (*core.ExternalCredential, error) {
	rec, err := s.tokenRecord(ctx, subjectID, integration, connection, instance)
	if err != nil {
		return nil, err
	}
	return s.recordToToken(rec)
}

func (s *LocalExternalCredentialProvider) ListCredentials(ctx context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	recs, err := s.store.Index("by_subject").GetAll(ctx, nil, subjectID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *LocalExternalCredentialProvider) ListCredentialsForProvider(ctx context.Context, subjectID, integration string) ([]*core.ExternalCredential, error) {
	recs, err := s.store.Index("by_subject_integration").GetAll(ctx, nil, subjectID, integration)
	if err != nil {
		return nil, fmt.Errorf("list tokens for integration: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *LocalExternalCredentialProvider) ListCredentialsForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.ExternalCredential, error) {
	recs, err := s.store.Index("by_subject_connection").GetAll(ctx, nil, subjectID, integration, connection)
	if err != nil {
		return nil, fmt.Errorf("list tokens for connection: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *LocalExternalCredentialProvider) DeleteCredential(ctx context.Context, id string) error {
	if id == "" {
		return s.store.Delete(ctx, id)
	}
	_, err := s.store.Get(ctx, id)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil
		}
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	return nil
}

func (s *LocalExternalCredentialProvider) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	return s.storeToken(ctx, token, false)
}

func (s *LocalExternalCredentialProvider) RestoreToken(ctx context.Context, token *core.IntegrationToken) error {
	return s.storeToken(ctx, token, true)
}

func (s *LocalExternalCredentialProvider) storeToken(ctx context.Context, token *core.IntegrationToken, preserveTimestamps bool) error {
	token.SubjectID = strings.TrimSpace(token.SubjectID)

	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt token pair: %w", err)
	}
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	now := time.Now()
	createdAt := tokenCreatedAt(token, now)
	updatedAt := tokenUpdatedAt(token, now, preserveTimestamps)
	fields := indexeddb.Record{
		"subject_id":              token.SubjectID,
		"integration":             token.Integration,
		"connection":              token.Connection,
		"instance":                token.Instance,
		"access_token_encrypted":  accessEnc,
		"refresh_token_encrypted": refreshEnc,
		"scopes":                  token.Scopes,
		"expires_at":              token.ExpiresAt,
		"last_refreshed_at":       token.LastRefreshedAt,
		"refresh_error_count":     token.RefreshErrorCount,
		"metadata_json":           token.MetadataJSON,
		"updated_at":              updatedAt,
	}

	existing, err := s.tokenRecord(ctx, token.SubjectID, token.Integration, token.Connection, token.Instance)
	switch err {
	case nil:
		token.ID = recString(existing, "id")
		fields["id"] = token.ID
		existingCreatedAt := recTime(existing, "created_at")
		if preserveTimestamps && !token.CreatedAt.IsZero() {
			existingCreatedAt = token.CreatedAt
		}
		if existingCreatedAt.IsZero() {
			existingCreatedAt = createdAt
		}
		fields["created_at"] = existingCreatedAt
		if err := s.store.Put(ctx, fields); err != nil {
			return fmt.Errorf("update token: %w", err)
		}
	case core.ErrNotFound:
		fields["id"] = token.ID
		fields["created_at"] = createdAt
		if err := s.store.Add(ctx, fields); err != nil {
			return fmt.Errorf("create token: %w", err)
		}
	default:
		return fmt.Errorf("check existing token: %w", err)
	}

	if err := s.deleteDuplicateLookupRecords(ctx, token.ID, token.SubjectID, token.Integration, token.Connection, token.Instance); err != nil {
		return err
	}
	return nil
}

func tokenCreatedAt(token *core.IntegrationToken, fallback time.Time) time.Time {
	if !token.CreatedAt.IsZero() {
		return token.CreatedAt
	}
	return fallback
}

func tokenUpdatedAt(token *core.IntegrationToken, fallback time.Time, preserve bool) time.Time {
	if preserve && !token.UpdatedAt.IsZero() {
		return token.UpdatedAt
	}
	return fallback
}

func (s *LocalExternalCredentialProvider) Token(ctx context.Context, subjectID, integration, connection, instance string) (*core.IntegrationToken, error) {
	return s.GetCredential(ctx, subjectID, integration, connection, instance)
}

func (s *LocalExternalCredentialProvider) ListTokens(ctx context.Context, subjectID string) ([]*core.IntegrationToken, error) {
	return s.ListCredentials(ctx, subjectID)
}

func (s *LocalExternalCredentialProvider) ListTokensForIntegration(ctx context.Context, subjectID, integration string) ([]*core.IntegrationToken, error) {
	return s.ListCredentialsForProvider(ctx, subjectID, integration)
}

func (s *LocalExternalCredentialProvider) ListTokensForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.IntegrationToken, error) {
	return s.ListCredentialsForConnection(ctx, subjectID, integration, connection)
}

func (s *LocalExternalCredentialProvider) DeleteToken(ctx context.Context, id string) error {
	return s.DeleteCredential(ctx, id)
}

func (s *LocalExternalCredentialProvider) recordToToken(rec indexeddb.Record) (*core.IntegrationToken, error) {
	access, refresh, err := s.enc.DecryptTokenPair(
		recString(rec, "access_token_encrypted"),
		recString(rec, "refresh_token_encrypted"),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt token pair: %v", errUnreadableStoredIntegrationToken, err)
	}
	return &core.IntegrationToken{
		ID:                recString(rec, "id"),
		SubjectID:         tokenRecordSubjectID(rec),
		Integration:       recString(rec, "integration"),
		Connection:        recString(rec, "connection"),
		Instance:          recString(rec, "instance"),
		AccessToken:       access,
		RefreshToken:      refresh,
		Scopes:            recString(rec, "scopes"),
		ExpiresAt:         recTimePtr(rec, "expires_at"),
		LastRefreshedAt:   recTimePtr(rec, "last_refreshed_at"),
		RefreshErrorCount: recInt(rec, "refresh_error_count"),
		MetadataJSON:      recString(rec, "metadata_json"),
		CreatedAt:         recTime(rec, "created_at"),
		UpdatedAt:         recTime(rec, "updated_at"),
	}, nil
}

func (s *LocalExternalCredentialProvider) recordsToTokens(recs []indexeddb.Record) ([]*core.IntegrationToken, error) {
	recs = dedupeTokenRecords(recs)
	out := make([]*core.IntegrationToken, 0, len(recs))
	for _, rec := range recs {
		t, err := s.recordToToken(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *LocalExternalCredentialProvider) tokenRecord(ctx context.Context, subjectID, integration, connection, instance string) (indexeddb.Record, error) {
	recs, err := s.store.Index("by_lookup").GetAll(ctx, nil, subjectID, integration, connection, instance)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	recs = dedupeTokenRecords(recs)
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}
	return recs[0], nil
}

func (s *LocalExternalCredentialProvider) deleteDuplicateLookupRecords(ctx context.Context, keepID, subjectID, integration, connection, instance string) error {
	recs, err := s.store.Index("by_lookup").GetAll(ctx, nil, subjectID, integration, connection, instance)
	if err != nil {
		return fmt.Errorf("list duplicate tokens: %w", err)
	}
	for _, rec := range recs {
		id := recString(rec, "id")
		if id == "" || id == keepID {
			continue
		}
		if err := s.store.Delete(ctx, id); err != nil && err != indexeddb.ErrNotFound {
			return fmt.Errorf("delete duplicate token %q: %w", id, err)
		}
	}
	return nil
}

func dedupeTokenRecords(recs []indexeddb.Record) []indexeddb.Record {
	if len(recs) <= 1 {
		return recs
	}

	bestByLookup := make(map[string]indexeddb.Record, len(recs))
	for _, rec := range recs {
		key := tokenLookupKey(rec)
		best, ok := bestByLookup[key]
		if !ok || tokenRecordLess(rec, best) {
			bestByLookup[key] = rec
		}
	}

	out := make([]indexeddb.Record, 0, len(bestByLookup))
	for _, rec := range bestByLookup {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return tokenRecordLess(out[i], out[j])
	})
	return out
}

func tokenLookupKey(rec indexeddb.Record) string {
	return tokenRecordSubjectID(rec) + "\x00" +
		recString(rec, "integration") + "\x00" +
		recString(rec, "connection") + "\x00" +
		recString(rec, "instance")
}

func tokenRecordSubjectID(rec indexeddb.Record) string {
	return strings.TrimSpace(recString(rec, "subject_id"))
}

func tokenRecordLess(a, b indexeddb.Record) bool {
	aUpdated := recTime(a, "updated_at")
	bUpdated := recTime(b, "updated_at")
	if !aUpdated.Equal(bUpdated) {
		return aUpdated.After(bUpdated)
	}

	aCreated := recTime(a, "created_at")
	bCreated := recTime(b, "created_at")
	if !aCreated.Equal(bCreated) {
		return aCreated.After(bCreated)
	}

	return recString(a, "id") < recString(b, "id")
}
