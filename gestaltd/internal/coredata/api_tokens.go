package coredata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type APITokenService struct {
	store           indexeddb.ObjectStore
	canonicalAccess *APITokenAccessService
	users           *UserService
}

func NewAPITokenService(ds indexeddb.IndexedDB, canonicalAccess *APITokenAccessService, users *UserService) *APITokenService {
	return &APITokenService{
		store:           ds.ObjectStore(StoreAPITokens),
		canonicalAccess: canonicalAccess,
		users:           users,
	}
}

func (s *APITokenService) StoreAPIToken(ctx context.Context, token *core.APIToken) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	ownerKind := strings.TrimSpace(token.OwnerKind)
	ownerID := strings.TrimSpace(token.OwnerID)
	if ownerKind == "" || ownerID == "" {
		return fmt.Errorf("store api token: owner_kind and owner_id are required")
	}
	if ownerKind != core.APITokenOwnerKindUser {
		return fmt.Errorf("store api token: unsupported owner_kind %q", ownerKind)
	}
	if token.CredentialSubjectID == "" && ownerKind == core.APITokenOwnerKindUser {
		token.CredentialSubjectID = apiTokenUserSubjectID(ownerID)
	}
	token.OwnerKind = ownerKind
	token.OwnerID = ownerID
	identityID, err := s.identityIDForToken(ctx, token)
	if err != nil {
		return err
	}
	token.IdentityID = identityID
	if token.TokenKind == "" {
		token.TokenKind = core.APITokenKindAPI
	}
	permissionsJSON, err := json.Marshal(token.Permissions)
	if err != nil {
		return fmt.Errorf("marshal api token permissions: %w", err)
	}
	now := time.Now()
	rec := indexeddb.Record{
		"id":                    token.ID,
		"identity_id":           identityID,
		"owner_kind":            ownerKind,
		"owner_id":              ownerID,
		"credential_subject_id": token.CredentialSubjectID,
		"token_kind":            token.TokenKind,
		"name":                  token.Name,
		"hashed_token":          token.HashedToken,
		"scopes":                token.Scopes,
		"permissions_json":      string(permissionsJSON),
		"expires_at":            token.ExpiresAt,
		"created_at":            now,
		"updated_at":            now,
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

func (s *APITokenService) ListAPITokensByIdentity(ctx context.Context, identityID string) ([]*core.APIToken, error) {
	recs, err := s.store.Index("by_identity").GetAll(ctx, nil, identityID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	out := make([]*core.APIToken, len(recs))
	for i, rec := range recs {
		out[i] = recordToAPIToken(rec)
	}
	return out, nil
}

func (s *APITokenService) ListAPITokensByOwner(ctx context.Context, ownerKind, ownerID string) ([]*core.APIToken, error) {
	recs, err := s.store.Index("by_owner").GetAll(ctx, nil, ownerKind, ownerID)
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
	return s.RevokeAPITokenByOwner(ctx, core.APITokenOwnerKindUser, userID, id)
}

func (s *APITokenService) RevokeAPITokenByIdentity(ctx context.Context, identityID, id string) error {
	deleted, err := s.store.Index("by_identity_id").Delete(ctx, id, identityID)
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

func (s *APITokenService) RevokeAPITokenByOwner(ctx context.Context, ownerKind, ownerID, id string) error {
	deleted, err := s.store.Index("by_owner_id").Delete(ctx, id, ownerKind, ownerID)
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

func (s *APITokenService) RevokeAllAPITokensByIdentity(ctx context.Context, identityID string) (int64, error) {
	tokensBefore, err := s.ListAPITokensByIdentity(ctx, identityID)
	if err != nil {
		return 0, err
	}
	deletedIDs := collectAPITokenIDs(tokensBefore)
	deleted, err := s.store.Index("by_identity").Delete(ctx, identityID)
	if err != nil {
		return 0, fmt.Errorf("revoke all api tokens: %w", err)
	}
	if s.canonicalAccess != nil && len(deletedIDs) > 0 {
		for _, id := range deletedIDs {
			if accessErr := s.canonicalAccess.ReplaceForToken(ctx, id, nil); accessErr != nil {
				return 0, fmt.Errorf("delete canonical api token access: %w", accessErr)
			}
		}
	}
	return deleted, nil
}

func (s *APITokenService) RevokeAllAPITokensByOwner(ctx context.Context, ownerKind, ownerID string) (int64, error) {
	var deletedIDs []string
	tokensBefore, listErr := s.ListAPITokensByOwner(ctx, ownerKind, ownerID)
	if listErr == nil {
		deletedIDs = collectAPITokenIDs(tokensBefore)
	}
	deleted, err := s.store.Index("by_owner").Delete(ctx, ownerKind, ownerID)
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
		ID:                  recString(rec, "id"),
		IdentityID:          recString(rec, "identity_id"),
		OwnerKind:           recString(rec, "owner_kind"),
		OwnerID:             recString(rec, "owner_id"),
		CredentialSubjectID: recString(rec, "credential_subject_id"),
		TokenKind:           recString(rec, "token_kind"),
		Name:                recString(rec, "name"),
		HashedToken:         recString(rec, "hashed_token"),
		Scopes:              recString(rec, "scopes"),
		ExpiresAt:           recTimePtr(rec, "expires_at"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
	if token.IdentityID == "" {
		if token.OwnerKind == core.APITokenOwnerKindUser {
			token.IdentityID = token.OwnerID
		}
	}
	if token.TokenKind == "" {
		token.TokenKind = core.APITokenKindAPI
	}
	if token.CredentialSubjectID == "" && token.OwnerKind == core.APITokenOwnerKindUser && token.OwnerID != "" {
		token.CredentialSubjectID = apiTokenUserSubjectID(token.OwnerID)
	}
	if raw := recString(rec, "permissions_json"); raw != "" {
		var permissions []core.AccessPermission
		if err := json.Unmarshal([]byte(raw), &permissions); err == nil {
			token.Permissions = permissions
		}
	}
	return token
}

func (s *APITokenService) identityIDForOwner(ctx context.Context, ownerKind, ownerID string) (string, error) {
	switch ownerKind {
	case core.APITokenOwnerKindUser:
		if s.users == nil {
			return ownerID, nil
		}
		return s.users.CanonicalIdentityIDForUser(ctx, ownerID)
	default:
		return "", fmt.Errorf("store api token: unsupported owner_kind %q", ownerKind)
	}
}

func (s *APITokenService) identityIDForToken(ctx context.Context, token *core.APIToken) (string, error) {
	if token == nil {
		return "", fmt.Errorf("store api token: token is required")
	}
	if token.IdentityID != "" {
		return token.IdentityID, nil
	}
	if token.OwnerKind == "" || token.OwnerID == "" {
		return "", fmt.Errorf("store api token: owner_kind and owner_id are required")
	}
	return s.identityIDForOwner(ctx, token.OwnerKind, token.OwnerID)
}

func (s *APITokenService) syncTokenAccess(ctx context.Context, token *core.APIToken) error {
	if s.canonicalAccess == nil || token == nil || token.ID == "" {
		return nil
	}
	permissions := apiTokenAccessPermissions(token)
	access := make([]core.APITokenAccess, 0, len(permissions))
	for _, perm := range permissions {
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

func apiTokenAccessPermissions(token *core.APIToken) []core.AccessPermission {
	if token == nil {
		return nil
	}
	if len(token.Permissions) > 0 {
		return append([]core.AccessPermission(nil), token.Permissions...)
	}
	seen := make(map[string]struct{})
	permissions := make([]core.AccessPermission, 0, len(strings.Fields(token.Scopes)))
	for _, scope := range strings.Fields(token.Scopes) {
		plugin := strings.TrimSpace(scope)
		if plugin == "" {
			continue
		}
		if _, ok := seen[plugin]; ok {
			continue
		}
		seen[plugin] = struct{}{}
		permissions = append(permissions, core.AccessPermission{Plugin: plugin})
	}
	return permissions
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

func apiTokenUserSubjectID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	return "user:" + userID
}
