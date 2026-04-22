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

type TokenService struct {
	store               indexeddb.ObjectStore
	enc                 *corecrypto.AESGCMEncryptor
	externalCredentials *ExternalCredentialService
}

var errUnreadableStoredIntegrationToken = errors.New("unreadable stored integration token")

func NewTokenService(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor, externalCredentials *ExternalCredentialService) *TokenService {
	return &TokenService{
		store:               ds.ObjectStore(StoreIntegrationTokens),
		enc:                 enc,
		externalCredentials: externalCredentials,
	}
}

func (s *TokenService) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	token.SubjectID = strings.TrimSpace(token.SubjectID)

	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt token pair: %w", err)
	}
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	now := time.Now()
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
		"updated_at":              now,
	}

	existing, err := s.tokenRecord(ctx, token.SubjectID, token.Integration, token.Connection, token.Instance)
	switch err {
	case nil:
		token.ID = recString(existing, "id")
		fields["id"] = token.ID
		createdAt := recTime(existing, "created_at")
		if createdAt.IsZero() {
			createdAt = now
		}
		fields["created_at"] = createdAt
		if err := s.store.Put(ctx, fields); err != nil {
			return fmt.Errorf("update token: %w", err)
		}
	case core.ErrNotFound:
		fields["id"] = token.ID
		fields["created_at"] = now
		if err := s.store.Add(ctx, fields); err != nil {
			return fmt.Errorf("create token: %w", err)
		}
	default:
		return fmt.Errorf("check existing token: %w", err)
	}

	if err := s.deleteDuplicateLookupRecords(ctx, token.ID, token.SubjectID, token.Integration, token.Connection, token.Instance); err != nil {
		return err
	}
	if err := s.syncExternalCredential(ctx, token, accessEnc, refreshEnc); err != nil {
		return err
	}
	return nil
}

func (s *TokenService) Token(ctx context.Context, subjectID, integration, connection, instance string) (*core.IntegrationToken, error) {
	rec, err := s.tokenRecord(ctx, subjectID, integration, connection, instance)
	if err != nil {
		return nil, err
	}
	return s.recordToToken(rec)
}

func (s *TokenService) ListTokens(ctx context.Context, subjectID string) ([]*core.IntegrationToken, error) {
	recs, err := s.store.Index("by_subject").GetAll(ctx, nil, subjectID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) ListTokensForIntegration(ctx context.Context, subjectID, integration string) ([]*core.IntegrationToken, error) {
	recs, err := s.store.Index("by_subject_integration").GetAll(ctx, nil, subjectID, integration)
	if err != nil {
		return nil, fmt.Errorf("list tokens for integration: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) ListTokensForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.IntegrationToken, error) {
	recs, err := s.store.Index("by_subject_connection").GetAll(ctx, nil, subjectID, integration, connection)
	if err != nil {
		return nil, fmt.Errorf("list tokens for connection: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) DeleteToken(ctx context.Context, id string) error {
	if id == "" {
		return s.store.Delete(ctx, id)
	}
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil
		}
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if s.externalCredentials != nil {
		subjectID := tokenRecordSubjectID(rec)
		remaining, err := s.store.Index("by_lookup").GetAll(ctx, nil, subjectID, recString(rec, "integration"), recString(rec, "connection"), recString(rec, "instance"))
		if err != nil {
			return fmt.Errorf("list remaining duplicate tokens: %w", err)
		}
		remaining = dedupeTokenRecords(remaining)
		if len(remaining) > 0 {
			if err := s.syncExternalCredentialRecord(ctx, remaining[0]); err != nil {
				return err
			}
		} else {
			if err := s.externalCredentials.DeleteCredential(ctx, subjectID, recString(rec, "integration"), recString(rec, "connection"), recString(rec, "instance")); err != nil && err != core.ErrNotFound {
				return fmt.Errorf("delete canonical external credential: %w", err)
			}
		}
	}
	return nil
}

func (s *TokenService) recordToToken(rec indexeddb.Record) (*core.IntegrationToken, error) {
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

func (s *TokenService) recordsToTokens(recs []indexeddb.Record) ([]*core.IntegrationToken, error) {
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

func (s *TokenService) tokenRecord(ctx context.Context, subjectID, integration, connection, instance string) (indexeddb.Record, error) {
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

func (s *TokenService) deleteDuplicateLookupRecords(ctx context.Context, keepID, subjectID, integration, connection, instance string) error {
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

func (s *TokenService) BackfillCanonicalCredentials(ctx context.Context) error {
	if s.externalCredentials == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list integration tokens for canonical backfill: %w", err)
	}
	for _, rec := range dedupeTokenRecords(recs) {
		if err := s.syncExternalCredentialRecord(ctx, rec); err != nil {
			if errors.Is(err, errUnreadableStoredIntegrationToken) {
				continue
			}
			return err
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

func (s *TokenService) syncExternalCredential(ctx context.Context, token *core.IntegrationToken, accessTokenEncrypted, refreshTokenEncrypted string) error {
	if s.externalCredentials == nil || token == nil || token.SubjectID == "" || token.Integration == "" || token.Connection == "" {
		return nil
	}
	subjectID := strings.TrimSpace(token.SubjectID)
	payloadEncrypted, err := encodeLegacyCredentialPayload(accessTokenEncrypted, refreshTokenEncrypted)
	if err != nil {
		return err
	}
	if _, err := s.externalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
		ID:                token.ID,
		SubjectID:         subjectID,
		Plugin:            token.Integration,
		Connection:        token.Connection,
		Instance:          token.Instance,
		AuthType:          externalCredentialAuthType(token),
		PayloadEncrypted:  payloadEncrypted,
		Scopes:            token.Scopes,
		ExpiresAt:         token.ExpiresAt,
		LastRefreshedAt:   token.LastRefreshedAt,
		RefreshErrorCount: token.RefreshErrorCount,
		MetadataJSON:      token.MetadataJSON,
		CreatedAt:         token.CreatedAt,
		UpdatedAt:         token.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("sync canonical external credential %q/%q/%q/%q: %w", subjectID, token.Integration, token.Connection, token.Instance, err)
	}
	return nil
}

func externalCredentialAuthType(token *core.IntegrationToken) string {
	if token == nil {
		return ""
	}
	if token.RefreshToken != "" {
		return "oauth2"
	}
	if token.AccessToken != "" {
		return "bearer"
	}
	return "manual"
}

func (s *TokenService) syncExternalCredentialRecord(ctx context.Context, rec indexeddb.Record) error {
	if s.externalCredentials == nil {
		return nil
	}
	subjectID := tokenRecordSubjectID(rec)
	plugin := recString(rec, "integration")
	connection := recString(rec, "connection")
	if subjectID == "" || plugin == "" || connection == "" {
		return nil
	}
	accessTokenEncrypted := recString(rec, "access_token_encrypted")
	refreshTokenEncrypted := recString(rec, "refresh_token_encrypted")
	payloadEncrypted, err := encodeLegacyCredentialPayload(accessTokenEncrypted, refreshTokenEncrypted)
	if err != nil {
		return err
	}
	token, err := s.recordToToken(rec)
	if err != nil {
		return fmt.Errorf("decode token for canonical external credential %q/%q/%q/%q: %w", subjectID, plugin, connection, recString(rec, "instance"), err)
	}
	if _, err := s.externalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
		ID:                recString(rec, "id"),
		SubjectID:         subjectID,
		Plugin:            plugin,
		Connection:        connection,
		Instance:          recString(rec, "instance"),
		AuthType:          externalCredentialAuthType(token),
		PayloadEncrypted:  payloadEncrypted,
		Scopes:            recString(rec, "scopes"),
		ExpiresAt:         recTimePtr(rec, "expires_at"),
		LastRefreshedAt:   recTimePtr(rec, "last_refreshed_at"),
		RefreshErrorCount: recInt(rec, "refresh_error_count"),
		MetadataJSON:      recString(rec, "metadata_json"),
		CreatedAt:         recTime(rec, "created_at"),
		UpdatedAt:         recTime(rec, "updated_at"),
	}); err != nil {
		return fmt.Errorf("sync canonical external credential record %q/%q/%q/%q: %w", subjectID, plugin, connection, recString(rec, "instance"), err)
	}
	return nil
}
