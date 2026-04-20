package coredata

import (
	"context"
	"encoding/json"
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
	users               *UserService
}

var errUnreadableStoredIntegrationToken = errors.New("unreadable stored integration token")

func NewTokenService(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor, externalCredentials *ExternalCredentialService, users *UserService) *TokenService {
	return &TokenService{
		store:               ds.ObjectStore(StoreIntegrationTokens),
		enc:                 enc,
		externalCredentials: externalCredentials,
		users:               users,
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
		"user_id":                 token.UserID,
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

	existing, err := s.tokenRecord(ctx, token.UserID, token.Integration, token.Connection, token.Instance)
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

	if err := s.deleteDuplicateLookupRecords(ctx, token.ID, token.UserID, token.Integration, token.Connection, token.Instance); err != nil {
		return err
	}
	if err := s.syncExternalCredential(ctx, token, accessEnc, refreshEnc); err != nil {
		return err
	}
	return nil
}

func (s *TokenService) Token(ctx context.Context, userID, integration, connection, instance string) (*core.IntegrationToken, error) {
	rec, err := s.tokenRecord(ctx, userID, integration, connection, instance)
	if err != nil {
		return nil, err
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

func (s *TokenService) IdentityToken(ctx context.Context, identityID, integration, connection, instance string) (*core.IntegrationToken, error) {
	if s.externalCredentials == nil {
		return nil, core.ErrNotFound
	}
	credential, err := s.externalCredentials.GetCredential(ctx, identityID, integration, connection, instance)
	if err != nil {
		return nil, err
	}
	return s.credentialToToken(credential)
}

func (s *TokenService) ListIdentityTokensForConnection(ctx context.Context, identityID, integration, connection string) ([]*core.IntegrationToken, error) {
	if s.externalCredentials == nil {
		return nil, nil
	}
	credentials, err := s.externalCredentials.ListByIdentityConnection(ctx, identityID, integration, connection)
	if err != nil {
		return nil, err
	}
	out := make([]*core.IntegrationToken, 0, len(credentials))
	for _, credential := range credentials {
		token, err := s.credentialToToken(credential)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *TokenService) StoreIdentityToken(ctx context.Context, token *core.IntegrationToken) error {
	if s.externalCredentials == nil {
		return fmt.Errorf("store identity token: external credentials store is not configured")
	}
	if token == nil {
		return fmt.Errorf("store identity token: token is required")
	}
	if strings.TrimSpace(token.IdentityID) == "" || strings.TrimSpace(token.Integration) == "" || strings.TrimSpace(token.Connection) == "" {
		return fmt.Errorf("store identity token: identity_id, integration, and connection are required")
	}
	accessEnc, refreshEnc, err := s.enc.EncryptTokenPair(token.AccessToken, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt token pair: %w", err)
	}
	payloadEncrypted, err := encodeLegacyCredentialPayload(accessEnc, refreshEnc)
	if err != nil {
		return err
	}
	now := time.Now()
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = now
	}
	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = now
	}
	credential := &core.ExternalCredential{
		ID:                token.ID,
		IdentityID:        strings.TrimSpace(token.IdentityID),
		Plugin:            strings.TrimSpace(token.Integration),
		Connection:        strings.TrimSpace(token.Connection),
		Instance:          strings.TrimSpace(token.Instance),
		AuthType:          externalCredentialAuthType(token),
		PayloadEncrypted:  payloadEncrypted,
		Scopes:            token.Scopes,
		ExpiresAt:         token.ExpiresAt,
		LastRefreshedAt:   token.LastRefreshedAt,
		RefreshErrorCount: token.RefreshErrorCount,
		MetadataJSON:      token.MetadataJSON,
		CreatedAt:         token.CreatedAt,
		UpdatedAt:         token.UpdatedAt,
	}
	if _, err := s.externalCredentials.UpsertCredential(ctx, credential); err != nil {
		return err
	}
	return nil
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
		remaining, err := s.store.Index("by_lookup").GetAll(ctx, nil, recString(rec, "user_id"), recString(rec, "integration"), recString(rec, "connection"), recString(rec, "instance"))
		if err != nil {
			return fmt.Errorf("list remaining duplicate tokens: %w", err)
		}
		remaining = dedupeTokenRecords(remaining)
		if len(remaining) > 0 {
			if err := s.syncExternalCredentialRecord(ctx, remaining[0]); err != nil {
				return err
			}
		} else {
			identityID, resolveErr := s.resolveIdentityID(ctx, recString(rec, "user_id"))
			if resolveErr == nil {
				if err := s.externalCredentials.DeleteCredential(ctx, identityID, recString(rec, "integration"), recString(rec, "connection"), recString(rec, "instance")); err != nil && err != core.ErrNotFound {
					return fmt.Errorf("delete canonical external credential: %w", err)
				}
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

func decodeLegacyCredentialPayload(payload string) (string, string, error) {
	if strings.TrimSpace(payload) == "" {
		return "", "", nil
	}
	var stored map[string]string
	if err := json.Unmarshal([]byte(payload), &stored); err != nil {
		return "", "", fmt.Errorf("decode credential payload: %w", err)
	}
	return stored["access_token_encrypted"], stored["refresh_token_encrypted"], nil
}

func (s *TokenService) credentialToToken(credential *core.ExternalCredential) (*core.IntegrationToken, error) {
	if credential == nil {
		return nil, core.ErrNotFound
	}
	accessEnc, refreshEnc, err := decodeLegacyCredentialPayload(credential.PayloadEncrypted)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUnreadableStoredIntegrationToken, err)
	}
	access, refresh, err := s.enc.DecryptTokenPair(accessEnc, refreshEnc)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt token pair: %v", errUnreadableStoredIntegrationToken, err)
	}
	return &core.IntegrationToken{
		ID:                credential.ID,
		IdentityID:        credential.IdentityID,
		Integration:       credential.Plugin,
		Connection:        credential.Connection,
		Instance:          credential.Instance,
		AccessToken:       access,
		RefreshToken:      refresh,
		Scopes:            credential.Scopes,
		ExpiresAt:         credential.ExpiresAt,
		LastRefreshedAt:   credential.LastRefreshedAt,
		RefreshErrorCount: credential.RefreshErrorCount,
		MetadataJSON:      credential.MetadataJSON,
		CreatedAt:         credential.CreatedAt,
		UpdatedAt:         credential.UpdatedAt,
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

func (s *TokenService) tokenRecord(ctx context.Context, userID, integration, connection, instance string) (indexeddb.Record, error) {
	recs, err := s.store.Index("by_lookup").GetAll(ctx, nil, userID, integration, connection, instance)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	recs = dedupeTokenRecords(recs)
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}
	return recs[0], nil
}

func (s *TokenService) deleteDuplicateLookupRecords(ctx context.Context, keepID, userID, integration, connection, instance string) error {
	recs, err := s.store.Index("by_lookup").GetAll(ctx, nil, userID, integration, connection, instance)
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
	return recString(rec, "user_id") + "\x00" +
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

func (s *TokenService) resolveIdentityID(ctx context.Context, userID string) (string, error) {
	if s.users == nil {
		return userID, nil
	}
	return s.users.CanonicalIdentityIDForUser(ctx, userID)
}

func (s *TokenService) syncExternalCredential(ctx context.Context, token *core.IntegrationToken, accessTokenEncrypted, refreshTokenEncrypted string) error {
	if s.externalCredentials == nil || token == nil || token.UserID == "" || token.Integration == "" || token.Connection == "" {
		return nil
	}
	identityID, resolveErr := s.resolveIdentityID(ctx, token.UserID)
	if resolveErr != nil {
		if errors.Is(resolveErr, core.ErrNotFound) {
			return nil
		}
		return resolveErr
	}
	payloadEncrypted, err := encodeLegacyCredentialPayload(accessTokenEncrypted, refreshTokenEncrypted)
	if err != nil {
		return err
	}
	if _, err := s.externalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
		ID:                token.ID,
		IdentityID:        identityID,
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
		return fmt.Errorf("sync canonical external credential %q/%q/%q/%q: %w", identityID, token.Integration, token.Connection, token.Instance, err)
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
	identityID, resolveErr := s.resolveIdentityID(ctx, recString(rec, "user_id"))
	if resolveErr != nil {
		if errors.Is(resolveErr, core.ErrNotFound) {
			return nil
		}
		return resolveErr
	}
	plugin := recString(rec, "integration")
	connection := recString(rec, "connection")
	if identityID == "" || plugin == "" || connection == "" {
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
		return fmt.Errorf("decode token for canonical external credential %q/%q/%q/%q: %w", identityID, plugin, connection, recString(rec, "instance"), err)
	}
	if _, err := s.externalCredentials.UpsertCredential(ctx, &core.ExternalCredential{
		ID:                recString(rec, "id"),
		IdentityID:        identityID,
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
		return fmt.Errorf("sync canonical external credential record %q/%q/%q/%q: %w", identityID, plugin, connection, recString(rec, "instance"), err)
	}
	return nil
}
