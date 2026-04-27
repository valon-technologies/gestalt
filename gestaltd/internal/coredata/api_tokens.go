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
	store indexeddb.ObjectStore
}

func NewAPITokenService(ds indexeddb.IndexedDB) *APITokenService {
	return &APITokenService{store: ds.ObjectStore(StoreAPITokens)}
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
	switch ownerKind {
	case core.APITokenOwnerKindUser:
		if token.CredentialSubjectID == "" {
			token.CredentialSubjectID = apiTokenUserSubjectID(ownerID)
		}
	case core.APITokenOwnerKindSubject:
		subjectKind, _, ok := core.ParseSubjectID(ownerID)
		if !ok {
			return fmt.Errorf("store api token: subject owner_id must be a canonical subject id")
		}
		if subjectKind == core.APITokenOwnerKindUser || subjectKind == "system" {
			return fmt.Errorf("store api token: subject owner_id must be a non-user, non-system subject id")
		}
		credentialSubjectID := strings.TrimSpace(token.CredentialSubjectID)
		if credentialSubjectID == "" {
			credentialSubjectID = ownerID
		}
		if credentialSubjectID != ownerID {
			return fmt.Errorf("store api token: subject-owned token credential_subject_id must match owner_id")
		}
		token.CredentialSubjectID = credentialSubjectID
	default:
		return fmt.Errorf("store api token: unsupported owner_kind %q", ownerKind)
	}
	token.OwnerKind = ownerKind
	token.OwnerID = ownerID
	permissionsJSON, err := json.Marshal(token.Permissions)
	if err != nil {
		return fmt.Errorf("marshal api token permissions: %w", err)
	}
	now := time.Now()
	rec := indexeddb.Record{
		"id":                    token.ID,
		"owner_kind":            ownerKind,
		"owner_id":              ownerID,
		"credential_subject_id": token.CredentialSubjectID,
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

func (s *APITokenService) RevokeAPITokenByOwner(ctx context.Context, ownerKind, ownerID, id string) error {
	deleted, err := s.store.Index("by_owner_id").Delete(ctx, id, ownerKind, ownerID)
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if deleted == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *APITokenService) RevokeAllAPITokens(ctx context.Context, userID string) (int64, error) {
	return s.RevokeAllAPITokensByOwner(ctx, core.APITokenOwnerKindUser, userID)
}

func (s *APITokenService) RevokeAllAPITokensByOwner(ctx context.Context, ownerKind, ownerID string) (int64, error) {
	deleted, err := s.store.Index("by_owner").Delete(ctx, ownerKind, ownerID)
	if err != nil {
		return 0, fmt.Errorf("revoke all api tokens: %w", err)
	}
	return deleted, nil
}

func recordToAPIToken(rec indexeddb.Record) *core.APIToken {
	token := &core.APIToken{
		ID:                  recString(rec, "id"),
		OwnerKind:           recString(rec, "owner_kind"),
		OwnerID:             recString(rec, "owner_id"),
		CredentialSubjectID: recString(rec, "credential_subject_id"),
		Name:                recString(rec, "name"),
		HashedToken:         recString(rec, "hashed_token"),
		Scopes:              recString(rec, "scopes"),
		ExpiresAt:           recTimePtr(rec, "expires_at"),
		CreatedAt:           recTime(rec, "created_at"),
		UpdatedAt:           recTime(rec, "updated_at"),
	}
	if token.CredentialSubjectID == "" && token.OwnerKind == core.APITokenOwnerKindUser && token.OwnerID != "" {
		token.CredentialSubjectID = apiTokenUserSubjectID(token.OwnerID)
	}
	if token.CredentialSubjectID == "" && token.OwnerKind == core.APITokenOwnerKindSubject && token.OwnerID != "" {
		token.CredentialSubjectID = token.OwnerID
	}
	if raw := recString(rec, "permissions_json"); raw != "" {
		var permissions []core.AccessPermission
		if err := json.Unmarshal([]byte(raw), &permissions); err == nil {
			token.Permissions = permissions
		}
	}
	return token
}

func apiTokenUserSubjectID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	return "user:" + userID
}
