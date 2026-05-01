package coredata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const (
	MCPOAuthGrantKindAuthorizationCode = "authorization_code"
	MCPOAuthGrantKindRefreshToken      = "refresh_token"
)

var (
	ErrMCPOAuthGrantNotFound = errors.New("mcp oauth grant: not found")
	ErrMCPOAuthGrantConsumed = errors.New("mcp oauth grant: consumed")
	ErrMCPOAuthGrantRevoked  = errors.New("mcp oauth grant: revoked")
	ErrMCPOAuthGrantExpired  = errors.New("mcp oauth grant: expired")
)

type MCPOAuthGrantService struct {
	db    indexeddb.IndexedDB
	store indexeddb.ObjectStore
	now   func() time.Time
}

func NewMCPOAuthGrantService(ds indexeddb.IndexedDB) *MCPOAuthGrantService {
	return &MCPOAuthGrantService{
		db:    ds,
		store: ds.ObjectStore(StoreMCPOAuthGrants),
		now:   time.Now,
	}
}

func (s *MCPOAuthGrantService) StoreAuthorizationCode(ctx context.Context, code string, expiresAt time.Time) error {
	if s == nil {
		return fmt.Errorf("store mcp oauth authorization code: service is not configured")
	}
	return s.addGrant(ctx, mcpOAuthGrantRecord{
		ID:        hashMCPOAuthGrantToken(code),
		Kind:      MCPOAuthGrantKindAuthorizationCode,
		ExpiresAt: expiresAt,
	})
}

func (s *MCPOAuthGrantService) ConsumeAuthorizationCode(ctx context.Context, code string) error {
	if s == nil {
		return fmt.Errorf("consume mcp oauth authorization code: service is not configured")
	}
	return s.consumeGrant(ctx, hashMCPOAuthGrantToken(code), MCPOAuthGrantKindAuthorizationCode)
}

func (s *MCPOAuthGrantService) ConsumeAuthorizationCodeAndStoreRefreshToken(ctx context.Context, code, refreshToken, familyID string, refreshExpiresAt time.Time) error {
	if s == nil {
		return fmt.Errorf("exchange mcp oauth authorization code: service is not configured")
	}
	if familyID == "" {
		return fmt.Errorf("exchange mcp oauth authorization code: refresh token family ID is required")
	}

	tx, err := s.db.Transaction(ctx, []string{StoreMCPOAuthGrants}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return fmt.Errorf("exchange mcp oauth authorization code: begin transaction: %w", err)
	}
	defer func() { _ = tx.Abort(ctx) }()

	store := tx.ObjectStore(StoreMCPOAuthGrants)
	now := s.now().UTC()
	codeRec, err := getMCPOAuthGrantRecord(ctx, store, hashMCPOAuthGrantToken(code))
	if err != nil {
		return err
	}
	if codeRec.Kind != MCPOAuthGrantKindAuthorizationCode {
		return ErrMCPOAuthGrantNotFound
	}
	if !codeRec.RevokedAt.IsZero() {
		return ErrMCPOAuthGrantRevoked
	}
	if !codeRec.ConsumedAt.IsZero() {
		return ErrMCPOAuthGrantConsumed
	}
	if !codeRec.ExpiresAt.IsZero() && now.After(codeRec.ExpiresAt) {
		return ErrMCPOAuthGrantExpired
	}

	codeRec.ConsumedAt = now
	codeRec.UpdatedAt = now
	if err := store.Put(ctx, codeRec.toIndexedDBRecord()); err != nil {
		return fmt.Errorf("exchange mcp oauth authorization code: consume code: %w", err)
	}
	if err := addMCPOAuthGrantRecord(ctx, store, mcpOAuthGrantRecord{
		ID:        hashMCPOAuthGrantToken(refreshToken),
		Kind:      MCPOAuthGrantKindRefreshToken,
		FamilyID:  familyID,
		ExpiresAt: refreshExpiresAt,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("exchange mcp oauth authorization code: commit: %w", err)
	}
	return nil
}

func (s *MCPOAuthGrantService) StoreRefreshToken(ctx context.Context, refreshToken, familyID string, expiresAt time.Time) error {
	if s == nil {
		return fmt.Errorf("store mcp oauth refresh token: service is not configured")
	}
	if familyID == "" {
		return fmt.Errorf("store mcp oauth refresh token: family ID is required")
	}
	return s.addGrant(ctx, mcpOAuthGrantRecord{
		ID:        hashMCPOAuthGrantToken(refreshToken),
		Kind:      MCPOAuthGrantKindRefreshToken,
		FamilyID:  familyID,
		ExpiresAt: expiresAt,
	})
}

func (s *MCPOAuthGrantService) RotateRefreshToken(ctx context.Context, oldToken, newToken, familyID string, newExpiresAt time.Time) error {
	if s == nil {
		return fmt.Errorf("rotate mcp oauth refresh token: service is not configured")
	}
	if familyID == "" {
		return fmt.Errorf("rotate mcp oauth refresh token: family ID is required")
	}

	tx, err := s.db.Transaction(ctx, []string{StoreMCPOAuthGrants}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return fmt.Errorf("rotate mcp oauth refresh token: begin transaction: %w", err)
	}
	defer func() { _ = tx.Abort(ctx) }()

	store := tx.ObjectStore(StoreMCPOAuthGrants)
	oldID := hashMCPOAuthGrantToken(oldToken)
	now := s.now().UTC()
	rec, err := getMCPOAuthGrantRecord(ctx, store, oldID)
	if err != nil {
		return err
	}
	if rec.Kind != MCPOAuthGrantKindRefreshToken || rec.FamilyID != familyID {
		return ErrMCPOAuthGrantNotFound
	}
	if !rec.RevokedAt.IsZero() {
		return ErrMCPOAuthGrantRevoked
	}
	if !rec.ConsumedAt.IsZero() {
		if err := revokeMCPOAuthGrantFamily(ctx, store, familyID, now); err != nil {
			return err
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return fmt.Errorf("rotate mcp oauth refresh token: commit reuse revocation: %w", commitErr)
		}
		return ErrMCPOAuthGrantConsumed
	}
	if !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt) {
		return ErrMCPOAuthGrantExpired
	}

	rec.ConsumedAt = now
	rec.UpdatedAt = now
	if err := store.Put(ctx, rec.toIndexedDBRecord()); err != nil {
		return fmt.Errorf("rotate mcp oauth refresh token: consume old token: %w", err)
	}
	if err := addMCPOAuthGrantRecord(ctx, store, mcpOAuthGrantRecord{
		ID:        hashMCPOAuthGrantToken(newToken),
		Kind:      MCPOAuthGrantKindRefreshToken,
		FamilyID:  familyID,
		ExpiresAt: newExpiresAt,
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rotate mcp oauth refresh token: commit: %w", err)
	}
	return nil
}

func (s *MCPOAuthGrantService) addGrant(ctx context.Context, rec mcpOAuthGrantRecord) error {
	now := s.now().UTC()
	if err := addMCPOAuthGrantRecord(ctx, s.store, rec, now); err != nil {
		return err
	}
	return nil
}

func (s *MCPOAuthGrantService) consumeGrant(ctx context.Context, id, kind string) error {
	tx, err := s.db.Transaction(ctx, []string{StoreMCPOAuthGrants}, indexeddb.TransactionReadwrite, indexeddb.TransactionOptions{})
	if err != nil {
		return fmt.Errorf("consume mcp oauth grant: begin transaction: %w", err)
	}
	defer func() { _ = tx.Abort(ctx) }()

	store := tx.ObjectStore(StoreMCPOAuthGrants)
	now := s.now().UTC()
	rec, err := getMCPOAuthGrantRecord(ctx, store, id)
	if err != nil {
		return err
	}
	if rec.Kind != kind {
		return ErrMCPOAuthGrantNotFound
	}
	if !rec.RevokedAt.IsZero() {
		return ErrMCPOAuthGrantRevoked
	}
	if !rec.ConsumedAt.IsZero() {
		return ErrMCPOAuthGrantConsumed
	}
	if !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt) {
		return ErrMCPOAuthGrantExpired
	}

	rec.ConsumedAt = now
	rec.UpdatedAt = now
	if err := store.Put(ctx, rec.toIndexedDBRecord()); err != nil {
		return fmt.Errorf("consume mcp oauth grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("consume mcp oauth grant: commit: %w", err)
	}
	return nil
}

type mcpOAuthGrantRecord struct {
	ID         string
	Kind       string
	FamilyID   string
	ExpiresAt  time.Time
	ConsumedAt time.Time
	RevokedAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (r mcpOAuthGrantRecord) toIndexedDBRecord() indexeddb.Record {
	return indexeddb.Record{
		"id":          r.ID,
		"kind":        r.Kind,
		"family_id":   r.FamilyID,
		"expires_at":  r.ExpiresAt,
		"consumed_at": zeroTimeToNil(r.ConsumedAt),
		"revoked_at":  zeroTimeToNil(r.RevokedAt),
		"created_at":  r.CreatedAt,
		"updated_at":  r.UpdatedAt,
	}
}

func mcpOAuthGrantRecordFromIndexedDB(rec indexeddb.Record) mcpOAuthGrantRecord {
	return mcpOAuthGrantRecord{
		ID:         recString(rec, "id"),
		Kind:       recString(rec, "kind"),
		FamilyID:   recString(rec, "family_id"),
		ExpiresAt:  recTime(rec, "expires_at"),
		ConsumedAt: recTime(rec, "consumed_at"),
		RevokedAt:  recTime(rec, "revoked_at"),
		CreatedAt:  recTime(rec, "created_at"),
		UpdatedAt:  recTime(rec, "updated_at"),
	}
}

func addMCPOAuthGrantRecord(ctx context.Context, store interface {
	Add(context.Context, indexeddb.Record) error
}, rec mcpOAuthGrantRecord, now time.Time) error {
	if rec.ID == "" {
		return fmt.Errorf("add mcp oauth grant: ID is required")
	}
	if rec.Kind == "" {
		return fmt.Errorf("add mcp oauth grant: kind is required")
	}
	if rec.ExpiresAt.IsZero() {
		return fmt.Errorf("add mcp oauth grant: expiration is required")
	}
	rec.CreatedAt = now
	rec.UpdatedAt = now
	if err := store.Add(ctx, rec.toIndexedDBRecord()); err != nil {
		if errors.Is(err, indexeddb.ErrAlreadyExists) {
			return ErrMCPOAuthGrantConsumed
		}
		return fmt.Errorf("add mcp oauth grant: %w", err)
	}
	return nil
}

func getMCPOAuthGrantRecord(ctx context.Context, store interface {
	Get(context.Context, string) (indexeddb.Record, error)
}, id string) (mcpOAuthGrantRecord, error) {
	rec, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return mcpOAuthGrantRecord{}, ErrMCPOAuthGrantNotFound
		}
		return mcpOAuthGrantRecord{}, fmt.Errorf("get mcp oauth grant: %w", err)
	}
	return mcpOAuthGrantRecordFromIndexedDB(rec), nil
}

func revokeMCPOAuthGrantFamily(ctx context.Context, store indexeddb.TransactionObjectStore, familyID string, revokedAt time.Time) error {
	recs, err := store.Index("by_family").GetAll(ctx, nil, familyID)
	if err != nil {
		return fmt.Errorf("revoke mcp oauth grant family: %w", err)
	}
	for _, raw := range recs {
		rec := mcpOAuthGrantRecordFromIndexedDB(raw)
		if rec.Kind != MCPOAuthGrantKindRefreshToken || rec.FamilyID != familyID {
			continue
		}
		if rec.RevokedAt.IsZero() {
			rec.RevokedAt = revokedAt
			rec.UpdatedAt = revokedAt
			if err := store.Put(ctx, rec.toIndexedDBRecord()); err != nil {
				return fmt.Errorf("revoke mcp oauth grant family: %w", err)
			}
		}
	}
	return nil
}

func hashMCPOAuthGrantToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func zeroTimeToNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
