package coredata

import (
	"context"
	"fmt"
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
	now := time.Now()
	fields := indexeddb.Record{
		"user_id":              token.UserID,
		"integration":          token.Integration,
		"connection":           token.Connection,
		"instance":             token.Instance,
		"access_token_sealed":  accessEnc,
		"refresh_token_sealed": refreshEnc,
		"scopes":               token.Scopes,
		"expires_at":           token.ExpiresAt,
		"last_refreshed_at":    token.LastRefreshedAt,
		"refresh_error_count":  token.RefreshErrorCount,
		"metadata_json":        token.MetadataJSON,
		"updated_at":           now,
	}

	_, err = s.store.Get(ctx, token.ID)
	if err == indexeddb.ErrNotFound {
		fields["id"] = token.ID
		fields["created_at"] = now
		return s.store.Add(ctx, fields)
	}
	if err != nil {
		return fmt.Errorf("check existing token: %w", err)
	}
	fields["id"] = token.ID
	return s.store.Put(ctx, fields)
}

func (s *TokenService) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	rec, err := s.store.Index("by_lookup").Get(ctx, userID, integration, connection, instance)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get token: %w", err)
	}
	return s.recordToToken(rec)
}

func (s *TokenService) ListTokens(ctx context.Context, userID string) ([]*core.IntegrationToken, error) {
	recs, err := s.store.Index("by_user").GetAll(ctx, nil, userID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) ListTokensForIntegration(ctx context.Context, userID, integration string) ([]*core.IntegrationToken, error) {
	recs, err := s.store.Index("by_user_integration").GetAll(ctx, nil, userID, integration)
	if err != nil {
		return nil, fmt.Errorf("list tokens for integration: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) ListTokensForConnection(ctx context.Context, userID, integration, connection string) ([]*core.IntegrationToken, error) {
	recs, err := s.store.Index("by_user_connection").GetAll(ctx, nil, userID, integration, connection)
	if err != nil {
		return nil, fmt.Errorf("list tokens for connection: %w", err)
	}
	return s.recordsToTokens(recs)
}

func (s *TokenService) DeleteToken(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

func (s *TokenService) recordToToken(rec indexeddb.Record) (*core.IntegrationToken, error) {
	access, refresh, err := s.enc.DecryptTokenPair(
		recString(rec, "access_token_sealed"),
		recString(rec, "refresh_token_sealed"),
	)
	if err != nil {
		return nil, fmt.Errorf("decrypt token pair: %w", err)
	}
	return &core.IntegrationToken{
		ID:                recString(rec, "id"),
		UserID:            recString(rec, "user_id"),
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
