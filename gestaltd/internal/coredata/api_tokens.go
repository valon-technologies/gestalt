package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type APITokenService struct {
	store           indexeddb.ObjectStore
	canonicalAccess *APITokenAccessService
}

func NewAPITokenService(ds indexeddb.IndexedDB, canonicalAccess *APITokenAccessService) *APITokenService {
	return &APITokenService{
		store:           ds.ObjectStore(StoreAPITokens),
		canonicalAccess: canonicalAccess,
	}
}

func (s *APITokenService) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	ownerKind := token.OwnerKind
	ownerID := token.OwnerID
	if ownerKind == "" && token.UserID != "" {
		ownerKind = core.APITokenOwnerKindUser
	}
	if ownerID == "" && token.UserID != "" {
		ownerID = token.UserID
	}
	permissionsJSON, err := json.Marshal(token.Permissions)
	if err != nil {
		return fmt.Errorf("marshal api token permissions: %w", err)
	}
	now := time.Now()
	rec := indexeddb.Record{
		"id":               token.ID,
		"user_id":          token.UserID,
		"owner_kind":       ownerKind,
		"owner_id":         ownerID,
		"name":             token.Name,
		"hashed_token":     token.HashedToken,
		"scopes":           token.Scopes,
		"permissions_json": string(permissionsJSON),
		"expires_at":       token.ExpiresAt,
		"created_at":       now,
		"updated_at":       now,
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return fmt.Errorf("store api token: %w", err)
	}
	if err := s.syncTokenAccess(ctx, token); err != nil {
		return err
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
	return s.ListAPITokensByOwner(ctx, core.APITokenOwnerKindUser, userID)
}

func (s *APITokenService) ListAPITokensByOwner(ctx context.Context, ownerKind, ownerID string) ([]*core.APIToken, error) {
	var (
		recs []indexeddb.Record
		err  error
	)
	if ownerKind == core.APITokenOwnerKindUser {
		ownerRecs, err := s.store.Index("by_owner").GetAll(ctx, nil, ownerKind, ownerID)
		if err != nil {
			return nil, fmt.Errorf("list api tokens: %w", err)
		}
		legacyRecs, err := s.store.Index("by_user").GetAll(ctx, nil, ownerID)
		if err != nil {
			return nil, fmt.Errorf("list api tokens: %w", err)
		}
		recs = mergeUniqueAPITokenRecords(ownerRecs, legacyRecs)
	} else {
		recs, err = s.store.Index("by_owner").GetAll(ctx, nil, ownerKind, ownerID)
		if err != nil {
			return nil, fmt.Errorf("list api tokens: %w", err)
		}
	}
	out := make([]*core.APIToken, len(recs))
	for i, rec := range recs {
		out[i] = recordToAPIToken(rec)
	}
	return out, nil
}

func mergeUniqueAPITokenRecords(recordSets ...[]indexeddb.Record) []indexeddb.Record {
	if len(recordSets) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	merged := make([]indexeddb.Record, 0)
	for _, recs := range recordSets {
		for _, rec := range recs {
			id := recString(rec, "id")
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			merged = append(merged, rec)
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func (s *APITokenService) RevokeAPIToken(ctx context.Context, userID, id string) error {
	return s.RevokeAPITokenByOwner(ctx, core.APITokenOwnerKindUser, userID, id)
}

func (s *APITokenService) RevokeAPITokenByOwner(ctx context.Context, ownerKind, ownerID, id string) error {
	var (
		deleted int64
		err     error
	)
	if ownerKind == core.APITokenOwnerKindUser {
		if deleted, err = s.store.Index("by_user_id").Delete(ctx, id, ownerID); err == nil && deleted > 0 {
			// Prefer the legacy user index when records were created before owner fields existed.
		} else if err == nil {
			deleted, err = s.store.Index("by_owner_id").Delete(ctx, id, ownerKind, ownerID)
		}
	} else {
		deleted, err = s.store.Index("by_owner_id").Delete(ctx, id, ownerKind, ownerID)
		if err == nil && deleted == 0 {
			tokens, listErr := s.ListAPITokensByOwner(ctx, ownerKind, ownerID)
			if listErr != nil {
				return fmt.Errorf("revoke api token: %w", listErr)
			}
			for _, token := range tokens {
				if token != nil && token.ID == id {
					if deleteErr := s.store.Delete(ctx, id); deleteErr != nil {
						return fmt.Errorf("revoke api token: %w", deleteErr)
					}
					deleted = 1
					break
				}
			}
		}
	}
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	if s.canonicalAccess != nil {
		if err := s.canonicalAccess.ReplaceForToken(ctx, id, nil); err != nil {
			return fmt.Errorf("delete canonical api token access: %w", err)
		}
	}
	return nil
}

func (s *APITokenService) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	return s.RevokeAllAPITokensByOwner(ctx, core.APITokenOwnerKindUser, userID)
}

func (s *APITokenService) RevokeAllAPITokensByOwner(ctx context.Context, ownerKind, ownerID string) (int64, error) {
	var deletedIDs []string
	var (
		deleted int64
		err     error
	)
	tokensBefore, listErr := s.ListAPITokensByOwner(ctx, ownerKind, ownerID)
	if listErr == nil {
		deletedIDs = collectAPITokenIDs(tokensBefore)
	}
	if ownerKind == core.APITokenOwnerKindUser {
		if deleted, err = s.store.Index("by_user").Delete(ctx, ownerID); err == nil && deleted > 0 {
			// Prefer the legacy user index when records were created before owner fields existed.
		} else if err == nil {
			deleted, err = s.store.Index("by_owner").Delete(ctx, ownerKind, ownerID)
		}
	} else {
		deleted, err = s.store.Index("by_owner").Delete(ctx, ownerKind, ownerID)
		if err == nil && deleted == 0 {
			tokens, listErr := s.ListAPITokensByOwner(ctx, ownerKind, ownerID)
			if listErr != nil {
				return 0, fmt.Errorf("revoke all api tokens: %w", listErr)
			}
			for _, token := range tokens {
				if token == nil || token.ID == "" {
					continue
				}
				if deleteErr := s.store.Delete(ctx, token.ID); deleteErr != nil {
					return 0, fmt.Errorf("revoke all api tokens: %w", deleteErr)
				}
				deleted++
			}
		}
	}
	if err != nil {
		return 0, fmt.Errorf("revoke all api tokens: %w", err)
	}
	if s.canonicalAccess != nil && len(deletedIDs) > 0 {
		remainingTokens, listErr := s.ListAPITokensByOwner(ctx, ownerKind, ownerID)
		if listErr != nil {
			return 0, fmt.Errorf("revoke all api tokens: %w", listErr)
		}
		remainingIDs := tokenIDSet(remainingTokens)
		for _, id := range deletedIDs {
			if _, ok := remainingIDs[id]; ok {
				continue
			}
			if accessErr := s.canonicalAccess.ReplaceForToken(ctx, id, nil); accessErr != nil {
				return 0, fmt.Errorf("delete canonical api token access: %w", accessErr)
			}
		}
	}
	return deleted, nil
}

func (s *APITokenService) BackfillTokenAccess(ctx context.Context) error {
	if s.canonicalAccess == nil {
		return nil
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("list api tokens for canonical backfill: %w", err)
	}
	for _, rec := range recs {
		if err := s.syncTokenAccess(ctx, recordToAPIToken(rec)); err != nil {
			return err
		}
	}
	return nil
}

func recordToAPIToken(rec indexeddb.Record) *core.APIToken {
	token := &core.APIToken{
		ID:          recString(rec, "id"),
		UserID:      recString(rec, "user_id"),
		OwnerKind:   recString(rec, "owner_kind"),
		OwnerID:     recString(rec, "owner_id"),
		Name:        recString(rec, "name"),
		HashedToken: recString(rec, "hashed_token"),
		Scopes:      recString(rec, "scopes"),
		ExpiresAt:   recTimePtr(rec, "expires_at"),
		CreatedAt:   recTime(rec, "created_at"),
		UpdatedAt:   recTime(rec, "updated_at"),
	}
	if token.OwnerKind == "" && token.UserID != "" {
		token.OwnerKind = core.APITokenOwnerKindUser
	}
	if token.OwnerID == "" && token.UserID != "" {
		token.OwnerID = token.UserID
	}
	if raw := recString(rec, "permissions_json"); raw != "" {
		var permissions []core.AccessPermission
		if err := json.Unmarshal([]byte(raw), &permissions); err == nil {
			token.Permissions = permissions
		}
	}
	return token
}

func (s *APITokenService) syncTokenAccess(ctx context.Context, token *core.APIToken) error {
	if s.canonicalAccess == nil || token == nil || token.ID == "" {
		return nil
	}
	access := make([]core.APITokenAccess, 0, len(token.Permissions))
	for _, perm := range token.Permissions {
		plugin := perm.Plugin
		if plugin == "" {
			continue
		}
		access = append(access, core.APITokenAccess{
			TokenID:             token.ID,
			Plugin:              plugin,
			InvokeAllOperations: len(perm.Operations) == 0,
			Operations:          append([]string(nil), perm.Operations...),
			ExpiresAt:           token.ExpiresAt,
			CreatedAt:           token.CreatedAt,
			UpdatedAt:           token.UpdatedAt,
		})
	}
	if err := s.canonicalAccess.ReplaceForToken(ctx, token.ID, access); err != nil {
		return fmt.Errorf("sync canonical api token access %q: %w", token.ID, err)
	}
	return nil
}

func collectAPITokenIDs(tokens []*core.APIToken) []string {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == nil || token.ID == "" {
			continue
		}
		out = append(out, token.ID)
	}
	return out
}

func tokenIDSet(tokens []*core.APIToken) map[string]struct{} {
	if len(tokens) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token == nil || token.ID == "" {
			continue
		}
		out[token.ID] = struct{}{}
	}
	return out
}
