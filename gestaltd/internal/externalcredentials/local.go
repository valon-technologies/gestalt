package externalcredentials

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

const (
	StoreName              = "external_credentials"
	removedLegacyStoreName = "integration_tokens"
)

var Schema = indexeddb.ObjectStoreSchema{
	Indexes: []indexeddb.IndexSchema{
		{Name: "by_subject", KeyPath: []string{"subject_id"}},
		{Name: "by_subject_integration", KeyPath: []string{"subject_id", "integration"}},
		{Name: "by_subject_connection", KeyPath: []string{"subject_id", "integration", "connection"}},
		{Name: "by_lookup", KeyPath: []string{"subject_id", "integration", "connection", "instance"}, Unique: true},
	},
	Columns: []indexeddb.ColumnDef{
		{Name: "id", Type: indexeddb.TypeString, PrimaryKey: true},
		{Name: "subject_id", Type: indexeddb.TypeString, NotNull: true},
		{Name: "integration", Type: indexeddb.TypeString, NotNull: true},
		{Name: "connection", Type: indexeddb.TypeString, NotNull: true},
		{Name: "instance", Type: indexeddb.TypeString},
		{Name: "access_token_encrypted", Type: indexeddb.TypeString},
		{Name: "refresh_token_encrypted", Type: indexeddb.TypeString},
		{Name: "scopes", Type: indexeddb.TypeString},
		{Name: "expires_at", Type: indexeddb.TypeTime},
		{Name: "last_refreshed_at", Type: indexeddb.TypeTime},
		{Name: "refresh_error_count", Type: indexeddb.TypeInt},
		{Name: "metadata_json", Type: indexeddb.TypeString},
		{Name: "created_at", Type: indexeddb.TypeTime},
		{Name: "updated_at", Type: indexeddb.TypeTime},
	},
}

var errUnreadableStoredExternalCredential = errors.New("unreadable stored external credential")

type LocalProvider struct {
	store indexeddb.ObjectStore
	enc   *corecrypto.AESGCMEncryptor
}

var _ core.ExternalCredentialProvider = (*LocalProvider)(nil)

func NewLocalProvider(ds indexeddb.IndexedDB, enc *corecrypto.AESGCMEncryptor) (*LocalProvider, error) {
	if ds == nil {
		return nil, fmt.Errorf("external credentials store: indexeddb is required")
	}
	if enc == nil {
		return nil, fmt.Errorf("external credentials store: encryptor is required")
	}
	ctx := context.Background()
	if err := ds.CreateObjectStore(ctx, StoreName, Schema); err != nil {
		return nil, fmt.Errorf("create %s store: %w", StoreName, err)
	}
	if err := failOnLegacyCredentialRows(ctx, ds); err != nil {
		return nil, err
	}
	provider := &LocalProvider{
		store: ds.ObjectStore(StoreName),
		enc:   enc,
	}
	return provider, nil
}

func (p *LocalProvider) PutCredential(ctx context.Context, credential *core.ExternalCredential) error {
	return p.storeCredential(ctx, credential, false)
}

func (p *LocalProvider) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) error {
	return p.storeCredential(ctx, credential, true)
}

func (p *LocalProvider) GetCredential(ctx context.Context, subjectID, integration, connection, instance string) (*core.ExternalCredential, error) {
	rec, err := p.credentialRecord(ctx, subjectID, integration, connection, instance)
	if err != nil {
		return nil, err
	}
	return p.recordToCredential(rec)
}

func (p *LocalProvider) ListCredentials(ctx context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	recs, err := p.listCredentialRecords(ctx, "by_subject", subjectID)
	if err != nil {
		return nil, fmt.Errorf("list external credentials: %w", err)
	}
	return p.recordsToCredentials(recs)
}

func (p *LocalProvider) ListCredentialsForProvider(ctx context.Context, subjectID, integration string) ([]*core.ExternalCredential, error) {
	recs, err := p.listCredentialRecords(ctx, "by_subject_integration", subjectID, integration)
	if err != nil {
		return nil, fmt.Errorf("list external credentials for integration: %w", err)
	}
	return p.recordsToCredentials(recs)
}

func (p *LocalProvider) ListCredentialsForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.ExternalCredential, error) {
	recs, err := p.listCredentialRecords(ctx, "by_subject_connection", subjectID, integration, connection)
	if err != nil {
		return nil, fmt.Errorf("list external credentials for connection: %w", err)
	}
	return p.recordsToCredentials(recs)
}

func (p *LocalProvider) DeleteCredential(ctx context.Context, id string) error {
	if id == "" {
		return p.store.Delete(ctx, id)
	}
	rec, err := getCredentialRecordByID(ctx, p.store, id)
	if err != nil {
		return err
	}
	if rec == nil {
		return nil
	}
	subjectID := credentialRecordSubjectID(rec)
	integration := recordString(rec, "integration")
	connection := recordString(rec, "connection")
	instance := recordString(rec, "instance")
	return deleteCredentialLookupRecords(ctx, p.store, subjectID, integration, connection, instance)
}

func (p *LocalProvider) storeCredential(ctx context.Context, credential *core.ExternalCredential, preserveTimestamps bool) error {
	if credential == nil {
		return fmt.Errorf("external credential is required")
	}
	credential.SubjectID = strings.TrimSpace(credential.SubjectID)

	accessEnc, refreshEnc, err := p.enc.EncryptTokenPair(credential.AccessToken, credential.RefreshToken)
	if err != nil {
		return fmt.Errorf("encrypt credential pair: %w", err)
	}
	if credential.ID == "" {
		credential.ID = uuid.New().String()
	}
	now := time.Now()
	createdAt := credentialCreatedAt(credential, now)
	updatedAt := credentialUpdatedAt(credential, now, preserveTimestamps)
	fields := indexeddb.Record{
		"subject_id":              credential.SubjectID,
		"integration":             credential.Integration,
		"connection":              credential.Connection,
		"instance":                credential.Instance,
		"access_token_encrypted":  accessEnc,
		"refresh_token_encrypted": refreshEnc,
		"scopes":                  credential.Scopes,
		"expires_at":              credential.ExpiresAt,
		"last_refreshed_at":       credential.LastRefreshedAt,
		"refresh_error_count":     credential.RefreshErrorCount,
		"metadata_json":           credential.MetadataJSON,
		"updated_at":              updatedAt,
	}

	existing, err := p.credentialRecord(ctx, credential.SubjectID, credential.Integration, credential.Connection, credential.Instance)
	switch {
	case err == nil:
		credential.ID = recordString(existing, "id")
		fields["id"] = credential.ID
		existingCreatedAt := recordTime(existing, "created_at")
		if preserveTimestamps && !credential.CreatedAt.IsZero() {
			existingCreatedAt = credential.CreatedAt
		}
		if existingCreatedAt.IsZero() {
			existingCreatedAt = createdAt
		}
		fields["created_at"] = existingCreatedAt
		if err := p.store.Put(ctx, fields); err != nil {
			return fmt.Errorf("update external credential: %w", err)
		}
	case errors.Is(err, core.ErrNotFound):
		fields["id"] = credential.ID
		fields["created_at"] = createdAt
		if err := p.store.Add(ctx, fields); err != nil {
			return fmt.Errorf("create external credential: %w", err)
		}
	default:
		return fmt.Errorf("check existing external credential: %w", err)
	}

	if err := p.deleteDuplicateLookupRecords(ctx, credential.ID, credential.SubjectID, credential.Integration, credential.Connection, credential.Instance); err != nil {
		return err
	}
	return nil
}

func credentialCreatedAt(credential *core.ExternalCredential, fallback time.Time) time.Time {
	if !credential.CreatedAt.IsZero() {
		return credential.CreatedAt
	}
	return fallback
}

func credentialUpdatedAt(credential *core.ExternalCredential, fallback time.Time, preserve bool) time.Time {
	if preserve && !credential.UpdatedAt.IsZero() {
		return credential.UpdatedAt
	}
	return fallback
}

func (p *LocalProvider) recordToCredential(rec indexeddb.Record) (*core.ExternalCredential, error) {
	access, refresh, err := p.enc.DecryptTokenPair(
		recordString(rec, "access_token_encrypted"),
		recordString(rec, "refresh_token_encrypted"),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt credential pair: %v", errUnreadableStoredExternalCredential, err)
	}
	return &core.ExternalCredential{
		ID:                recordString(rec, "id"),
		SubjectID:         credentialRecordSubjectID(rec),
		Integration:       recordString(rec, "integration"),
		Connection:        recordString(rec, "connection"),
		Instance:          recordString(rec, "instance"),
		AccessToken:       access,
		RefreshToken:      refresh,
		Scopes:            recordString(rec, "scopes"),
		ExpiresAt:         recordTimePtr(rec, "expires_at"),
		LastRefreshedAt:   recordTimePtr(rec, "last_refreshed_at"),
		RefreshErrorCount: recordInt(rec, "refresh_error_count"),
		MetadataJSON:      recordString(rec, "metadata_json"),
		CreatedAt:         recordTime(rec, "created_at"),
		UpdatedAt:         recordTime(rec, "updated_at"),
	}, nil
}

func (p *LocalProvider) recordsToCredentials(recs []indexeddb.Record) ([]*core.ExternalCredential, error) {
	recs = dedupeCredentialRecords(recs)
	out := make([]*core.ExternalCredential, 0, len(recs))
	for _, rec := range recs {
		credential, err := p.recordToCredential(rec)
		if err != nil {
			return nil, err
		}
		out = append(out, credential)
	}
	return out, nil
}

func (p *LocalProvider) credentialRecord(ctx context.Context, subjectID, integration, connection, instance string) (indexeddb.Record, error) {
	recs, err := p.listCredentialRecords(ctx, "by_lookup", subjectID, integration, connection, instance)
	if err != nil {
		return nil, fmt.Errorf("get external credential: %w", err)
	}
	if len(recs) == 0 {
		return nil, core.ErrNotFound
	}
	return recs[0], nil
}

func (p *LocalProvider) listCredentialRecords(ctx context.Context, indexName string, keys ...any) ([]indexeddb.Record, error) {
	recs, err := p.store.Index(indexName).GetAll(ctx, nil, keys...)
	if err != nil {
		return nil, err
	}
	return dedupeCredentialRecords(recs), nil
}

func (p *LocalProvider) deleteDuplicateLookupRecords(ctx context.Context, keepID, subjectID, integration, connection, instance string) error {
	recs, err := p.store.Index("by_lookup").GetAll(ctx, nil, subjectID, integration, connection, instance)
	if err != nil {
		return fmt.Errorf("list duplicate external credentials: %w", err)
	}
	for _, rec := range recs {
		id := recordString(rec, "id")
		if id == "" || id == keepID {
			continue
		}
		if err := p.store.Delete(ctx, id); err != nil && !errors.Is(err, indexeddb.ErrNotFound) {
			return fmt.Errorf("delete duplicate external credential %q: %w", id, err)
		}
	}
	return nil
}

func dedupeCredentialRecords(recs []indexeddb.Record) []indexeddb.Record {
	if len(recs) <= 1 {
		return recs
	}

	bestByLookup := make(map[string]indexeddb.Record, len(recs))
	for _, rec := range recs {
		key := credentialLookupKey(rec)
		best, ok := bestByLookup[key]
		if !ok || credentialRecordLess(rec, best) {
			bestByLookup[key] = rec
		}
	}

	out := make([]indexeddb.Record, 0, len(bestByLookup))
	for _, rec := range bestByLookup {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return credentialRecordLess(out[i], out[j])
	})
	return out
}

func credentialLookupKey(rec indexeddb.Record) string {
	return credentialRecordSubjectID(rec) + "\x00" +
		recordString(rec, "integration") + "\x00" +
		recordString(rec, "connection") + "\x00" +
		recordString(rec, "instance")
}

func credentialRecordSubjectID(rec indexeddb.Record) string {
	return strings.TrimSpace(recordString(rec, "subject_id"))
}

func credentialRecordLess(a, b indexeddb.Record) bool {
	aUpdated := recordTime(a, "updated_at")
	bUpdated := recordTime(b, "updated_at")
	if !aUpdated.Equal(bUpdated) {
		return aUpdated.After(bUpdated)
	}

	aCreated := recordTime(a, "created_at")
	bCreated := recordTime(b, "created_at")
	if !aCreated.Equal(bCreated) {
		return aCreated.After(bCreated)
	}

	return recordString(a, "id") < recordString(b, "id")
}

func deleteCredentialRecord(ctx context.Context, store indexeddb.ObjectStore, id string) error {
	if store == nil {
		return nil
	}
	_, err := store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, indexeddb.ErrNotFound) {
			return nil
		}
		return err
	}
	return store.Delete(ctx, id)
}

func getCredentialRecordByID(ctx context.Context, store indexeddb.ObjectStore, id string) (indexeddb.Record, error) {
	if store == nil {
		return nil, nil
	}
	rec, err := store.Get(ctx, id)
	if errors.Is(err, indexeddb.ErrNotFound) {
		return nil, nil
	}
	return rec, err
}

func deleteCredentialLookupRecords(ctx context.Context, store indexeddb.ObjectStore, subjectID, integration, connection, instance string) error {
	if store == nil {
		return nil
	}
	recs, err := store.Index("by_lookup").GetAll(ctx, nil, subjectID, integration, connection, instance)
	if errors.Is(err, indexeddb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, rec := range recs {
		id := recordString(rec, "id")
		if id == "" {
			continue
		}
		errs = append(errs, deleteCredentialRecord(ctx, store, id))
	}
	return errors.Join(errs...)
}

func failOnLegacyCredentialRows(ctx context.Context, ds indexeddb.IndexedDB) error {
	exists, err := objectStoreExists(ctx, ds, removedLegacyStoreName)
	if err != nil {
		return fmt.Errorf("check legacy %s store: %w", removedLegacyStoreName, err)
	}
	if !exists {
		return nil
	}
	count, err := ds.ObjectStore(removedLegacyStoreName).Count(ctx, nil)
	switch {
	case err == nil:
	case errors.Is(err, indexeddb.ErrNotFound):
		return nil
	default:
		return fmt.Errorf("count legacy %s rows: %w", removedLegacyStoreName, err)
	}
	if count == 0 {
		return nil
	}
	return fmt.Errorf("legacy %s store still contains %d rows; run the manual external_credentials migration before starting gestaltd", removedLegacyStoreName, count)
}

func objectStoreExists(ctx context.Context, db indexeddb.IndexedDB, storeName string) (bool, error) {
	if storeName == "" {
		return false, nil
	}
	type objectStoreExistenceChecker interface {
		HasObjectStore(name string) bool
	}
	if checker, ok := db.(objectStoreExistenceChecker); ok {
		return checker.HasObjectStore(storeName), nil
	}
	_, err := db.ObjectStore(storeName).Count(ctx, nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, indexeddb.ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}

func recordString(rec indexeddb.Record, key string) string {
	value, _ := rec[key].(string)
	return value
}

func recordInt(rec indexeddb.Record, key string) int {
	switch value := rec[key].(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func recordTime(rec indexeddb.Record, key string) time.Time {
	value, ok := rec[key]
	if !ok || value == nil {
		return time.Time{}
	}
	switch raw := value.(type) {
	case time.Time:
		return raw
	case *time.Time:
		if raw == nil {
			return time.Time{}
		}
		return *raw
	default:
		return time.Time{}
	}
}

func recordTimePtr(rec indexeddb.Record, key string) *time.Time {
	value := recordTime(rec, key)
	if value.IsZero() {
		return nil
	}
	return &value
}
