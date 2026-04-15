package coredata

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	corecrypto "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type TokenService struct {
	store indexeddb.ObjectStore
	enc   *corecrypto.AESGCMEncryptor
}

func NewTokenService(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) *TokenService {
	return &TokenService{
		store: ds.ObjectStore(StoreIntegrationTokens),
		enc:   enc,
	}
}

func (s *TokenService) StoreToken(ctx context.Context, token *core.IntegrationToken) error {
	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt token pair: %w", err)
	}
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	ownerKind := integrationTokenOwnerKind(token)
	ownerID := integrationTokenOwnerID(token)
	storedUserID := integrationTokenStoredUserID(token, ownerKind, ownerID)
	now := time.Now()
	fields := indexeddb.Record{
		"user_id":                 storedUserID,
		"owner_kind":              ownerKind,
		"owner_id":                ownerID,
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

	existing, err := s.tokenRecordByOwner(ctx, ownerKind, ownerID, token.Integration, token.Connection, token.Instance)
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

	if err := s.deleteDuplicateLookupRecords(ctx, token.ID, ownerKind, ownerID, token.Integration, token.Connection, token.Instance); err != nil {
		return err
	}
	return nil
}

func (s *TokenService) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	return s.TokenByOwner(ctx, core.IntegrationTokenOwnerKindUser, userID, integration, connection, instance)
}

func (s *TokenService) TokenByOwner(ctx context.Context, ownerKind, ownerID, integration, connection, instance string) (*core.IntegrationToken, error) {
	rec, err := s.tokenRecordByOwner(ctx, ownerKind, ownerID, integration, connection, instance)
	if err != nil {
		return nil, err
	}
	return s.recordToToken(rec)
}

func (s *TokenService) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	return s.ListTokensByOwner(ctx, core.IntegrationTokenOwnerKindUser, userID)
}

func (s *TokenService) ListTokensByOwner(ctx context.Context, ownerKind, ownerID string) ([]*core.IntegrationToken, error) {
	var (
		recs []indexeddb.Record
		err  error
	)
	if ownerKind == core.IntegrationTokenOwnerKindUser {
		recs, err = s.store.Index("by_user").GetAll(ctx, nil, ownerID)
		if err == nil && len(recs) == 0 {
			recs, err = s.store.Index("by_owner").GetAll(ctx, nil, ownerKind, ownerID)
		}
	} else {
		recs, err = s.store.Index("by_owner").GetAll(ctx, nil, ownerKind, ownerID)
	}
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	return s.ListTokensForIntegrationByOwner(ctx, core.IntegrationTokenOwnerKindUser, userID, integration)
}

func (s *TokenService) ListTokensForIntegrationByOwner(ctx context.Context, ownerKind, ownerID, integration string) ([]*core.IntegrationToken, error) {
	var (
		recs []indexeddb.Record
		err  error
	)
	if ownerKind == core.IntegrationTokenOwnerKindUser {
		recs, err = s.store.Index("by_user_integration").GetAll(ctx, nil, ownerID, integration)
		if err == nil && len(recs) == 0 {
			recs, err = s.store.Index("by_owner_integration").GetAll(ctx, nil, ownerKind, ownerID, integration)
		}
	} else {
		recs, err = s.store.Index("by_owner_integration").GetAll(ctx, nil, ownerKind, ownerID, integration)
	}
	if err != nil {
		return nil, fmt.Errorf("list tokens for integration: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	return s.ListTokensForConnectionByOwner(ctx, core.IntegrationTokenOwnerKindUser, userID, integration, connection)
}

func (s *TokenService) ListTokensForConnectionByOwner(ctx context.Context, ownerKind, ownerID, integration, connection string) ([]*core.IntegrationToken, error) {
	var (
		recs []indexeddb.Record
		err  error
	)
	if ownerKind == core.IntegrationTokenOwnerKindUser {
		recs, err = s.store.Index("by_user_connection").GetAll(ctx, nil, ownerID, integration, connection)
		if err == nil && len(recs) == 0 {
			recs, err = s.store.Index("by_owner_connection").GetAll(ctx, nil, ownerKind, ownerID, integration, connection)
		}
	} else {
		recs, err = s.store.Index("by_owner_connection").GetAll(ctx, nil, ownerKind, ownerID, integration, connection)
	}
	if err != nil {
		return nil, fmt.Errorf("list tokens for connection: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) DeleteToken(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

func (s *TokenService) DeleteAllTokensByOwner(ctx context.Context, ownerKind, ownerID string) (int64, error) {
	var (
		deleted int64
		err     error
	)
	if ownerKind == core.IntegrationTokenOwnerKindUser {
		deleted, err = s.store.Index("by_user").Delete(ctx, ownerID)
		if err == nil && deleted == 0 {
			deleted, err = s.store.Index("by_owner").Delete(ctx, ownerKind, ownerID)
		}
	} else {
		deleted, err = s.store.Index("by_owner").Delete(ctx, ownerKind, ownerID)
		if err == nil && deleted == 0 {
			tokens, listErr := s.ListTokensByOwner(ctx, ownerKind, ownerID)
			if listErr != nil {
				return 0, fmt.Errorf("delete tokens by owner: %w", listErr)
			}
			for _, token := range tokens {
				if token == nil || token.ID == "" {
					continue
				}
				if deleteErr := s.store.Delete(ctx, token.ID); deleteErr != nil {
					return 0, fmt.Errorf("delete tokens by owner: %w", deleteErr)
				}
				deleted++
			}
		}
	}
	if err != nil {
		return 0, fmt.Errorf("delete tokens by owner: %w", err)
	}
	return deleted, nil
}

func (s *TokenService) recordToToken(rec indexeddb.Record) (*core.IntegrationToken, error) {
	access, refresh, err := s.enc.DecryptTokenPair(
		recString(rec, "access_token_encrypted"),
		recString(rec, "refresh_token_encrypted"),
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt token pair: %w", err)
	}
	token := &core.IntegrationToken{
		ID:                recString(rec, "id"),
		UserID:            recString(rec, "user_id"),
		OwnerKind:         recString(rec, "owner_kind"),
		OwnerID:           recString(rec, "owner_id"),
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
	}
	if token.OwnerKind == "" && token.UserID != "" {
		token.OwnerKind = core.IntegrationTokenOwnerKindUser
	}
	if token.OwnerID == "" && token.OwnerKind == core.IntegrationTokenOwnerKindUser && token.UserID != "" {
		token.OwnerID = token.UserID
	}
	if token.OwnerKind != "" && token.OwnerKind != core.IntegrationTokenOwnerKindUser {
		token.UserID = ""
	}
	return token, nil
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

func (s *TokenService) tokenRecordByOwner(ctx context.Context, ownerKind, ownerID, integration, connection, instance string) (indexeddb.Record, error) {
	storedUserID := core.IntegrationTokenStoredUserID(ownerKind, ownerID)
	recs, err := s.store.Index("by_lookup").GetAll(ctx, nil, storedUserID, integration, connection, instance)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	recs = dedupeTokenRecords(recs)
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}
	return recs[0], nil
}

func (s *TokenService) deleteDuplicateLookupRecords(ctx context.Context, keepID, ownerKind, ownerID, integration, connection, instance string) error {
	storedUserID := core.IntegrationTokenStoredUserID(ownerKind, ownerID)
	recs, err := s.store.Index("by_lookup").GetAll(ctx, nil, storedUserID, integration, connection, instance)
	if err != nil {
		return fmt.Errorf("list duplicate tokens: %w", err)
	}
	recs = dedupeTokenRecords(recs)
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
	return tokenRecordOwnerKind(rec) + "\x00" +
		tokenRecordOwnerID(rec) + "\x00" +
		recString(rec, "integration") + "\x00" +
		recString(rec, "connection") + "\x00" +
		recString(rec, "instance")
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

func integrationTokenOwnerKind(token *core.IntegrationToken) string {
	if token == nil {
		return ""
	}
	if token.OwnerKind != "" {
		return token.OwnerKind
	}
	if token.UserID != "" {
		return core.IntegrationTokenOwnerKindUser
	}
	return ""
}

func integrationTokenOwnerID(token *core.IntegrationToken) string {
	if token == nil {
		return ""
	}
	if token.OwnerID != "" {
		return token.OwnerID
	}
	if token.UserID != "" {
		return token.UserID
	}
	return ""
}

func integrationTokenStoredUserID(token *core.IntegrationToken, ownerKind, ownerID string) string {
	if token == nil {
		return core.IntegrationTokenStoredUserID(ownerKind, ownerID)
	}
	if token.UserID != "" {
		return token.UserID
	}
	return core.IntegrationTokenStoredUserID(ownerKind, ownerID)
}

func tokenRecordOwnerKind(rec indexeddb.Record) string {
	if ownerKind := recString(rec, "owner_kind"); ownerKind != "" {
		return ownerKind
	}
	if recString(rec, "user_id") != "" {
		return core.IntegrationTokenOwnerKindUser
	}
	return ""
}

func tokenRecordOwnerID(rec indexeddb.Record) string {
	if ownerID := recString(rec, "owner_id"); ownerID != "" {
		return ownerID
	}
	return recString(rec, "user_id")
}
