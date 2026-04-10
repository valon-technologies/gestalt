package coredata

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type APITokenService struct {
	store indexeddb.ObjectStore
}

func NewAPITokenService(ds indexeddb.IndexedDB) *APITokenService {
	return &APITokenService{store: ds.ObjectStore(StoreAPITokens)}
}

func (s *APITokenService) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	now := time.Now()
	rec := indexeddb.Record{
		"id":           token.ID,
		"user_id":      token.UserID,
		"name":         token.Name,
		"hashed_token": token.HashedToken,
		"scopes":       token.Scopes,
		"expires_at":   token.ExpiresAt,
		"created_at":   now,
		"updated_at":   now,
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return fmt.Errorf("store api token: %w", err)
	}
	return nil
}

func (s *APITokenService) ValidateAPIToken(ctx context.Context, hashedToken string) (*core.APIToken, error) {
	rec, err := s.store.Index("by_hash").Get(ctx, hashedToken)
	if err != nil {
		if err == indexeddb.ErrNotFound {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("validate api token: %w", err)
	}
	token := recordToAPIToken(rec)
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, core.ErrNotFound
	}
	return token, nil
}

func (s *APITokenService) ListAPITokens(ctx context.Context, userID string) ([]*core.APIToken, error) {
	recs, err := s.store.Index("by_user").GetAll(ctx, nil, userID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	out := make([]*core.APIToken, len(recs))
	for i, rec := range recs {
		out[i] = recordToAPIToken(rec)
	}
	return out, nil
}

func (s *APITokenService) RevokeAPIToken(ctx context.Context, userID, id string) error {
	deleted, err := s.store.Index("by_user_id").Delete(ctx, id, userID)
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *APITokenService) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	deleted, err := s.store.Index("by_user").Delete(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke all api tokens: %w", err)
	}
	return deleted, nil
}

func recordToAPIToken(rec indexeddb.Record) *core.APIToken {
	return &core.APIToken{
		ID:          recString(rec, "id"),
		UserID:      recString(rec, "user_id"),
		Name:        recString(rec, "name"),
		HashedToken: recString(rec, "hashed_token"),
		Scopes:      recString(rec, "scopes"),
		ExpiresAt:   recTimePtr(rec, "expires_at"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
}
