package coredata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

var (
	ErrSCIMConfigNotFound = errors.New("SCIM client configuration not found")
	ErrSCIMConfigConflict = errors.New("SCIM client configuration was modified concurrently")
	ErrSCIMClientExists   = errors.New("SCIM client already exists")
)

// SCIMClientRecord is runtime ownership metadata plus non-secret SCIM config.
// Bearer tokens are deliberately absent: encrypted credentials live in
// scim_secret, keyed by client and credential ID.
type SCIMClientRecord struct {
	SCIMClientData
	ID        string
	Enabled   bool
	Retained  bool
	CreatedAt time.Time
	UpdatedAt time.Time
	UpdatedBy string
	Revision  int64
}

// SCIMClientData is the non-secret client configuration shared by config.yaml
// and runtime storage. It is deliberately independent of internal/config so
// internal packages can use it without importing service-layer packages.
type SCIMClientData struct {
	AuthoritativeUserDomains []string
	ActiveUserRelationships  []SCIMRelationshipData
	CredentialIDs            []string
}

type SCIMRelationshipData struct {
	Relation     string
	ResourceType string
	ResourceID   string
}

// SCIMClientSecret is one encrypted bearer credential. Ciphertext is opaque to
// this service; the admin runtime encrypts and decrypts at its boundary.
type SCIMClientSecret struct {
	ClientID     string
	CredentialID string
	Ciphertext   []byte
	Revision     int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	UpdatedBy    string
}

type SCIMConfigService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewSCIMConfigService(ds indexeddb.IndexedDB) *SCIMConfigService {
	return &SCIMConfigService{db: ds, store: ds.ObjectStore(StoreSCIMConfig)}
}

// List returns enabled and retained clients in stable client-ID order.
func (s *SCIMConfigService) List(ctx context.Context) ([]*SCIMClientRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("SCIM config service is not configured")
	}
	records, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list SCIM config: %w", err)
	}
	out := make([]*SCIMClientRecord, 0, len(records))
	for _, record := range records {
		if client := recordToSCIMClient(record); client != nil {
			out = append(out, client)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *SCIMConfigService) Get(ctx context.Context, clientID string) (*SCIMClientRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("SCIM config service is not configured")
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, fmt.Errorf("SCIM client id is required")
	}
	rec, err := s.store.Get(ctx, clientID)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrSCIMConfigNotFound
		}
		return nil, fmt.Errorf("get SCIM config: %w", err)
	}
	client := recordToSCIMClient(rec)
	if client == nil {
		return nil, ErrSCIMConfigNotFound
	}
	return client, nil
}

type PutSCIMClientInput struct {
	Client             *SCIMClientRecord
	Secrets            []SCIMClientSecret
	Actor              string
	RequireRevision    int64
	RequireRevisionSet bool
}

// Put writes the client and its encrypted credentials in one IndexedDB
// transaction. Read, revision comparison, and write happen inside that
// transaction so concurrent writers cannot both advance the same revision.
func (s *SCIMConfigService) Put(ctx context.Context, input PutSCIMClientInput) (*SCIMClientRecord, error) {
	if s == nil || s.db == nil || s.store == nil {
		return nil, fmt.Errorf("SCIM config service is not configured")
	}
	if input.Client == nil {
		return nil, fmt.Errorf("SCIM client is required")
	}
	client := *input.Client
	client.ID = strings.TrimSpace(client.ID)
	if client.ID == "" {
		return nil, fmt.Errorf("SCIM client id is required")
	}
	now := time.Now().UTC().Truncate(time.Millisecond)

	tx, err := s.db.Transaction(ctx, []string{StoreSCIMConfig, StoreSCIMSecrets}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("SCIM config write: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	clients := tx.ObjectStore(StoreSCIMConfig)
	secrets := tx.ObjectStore(StoreSCIMSecrets)

	existingRec, err := clients.Get(ctx, client.ID)
	switch {
	case err == nil:
		existing := recordToSCIMClient(existingRec)
		if existing == nil {
			return nil, fmt.Errorf("SCIM config record is invalid")
		}
		if input.RequireRevisionSet && existing.Revision != input.RequireRevision {
			return nil, ErrSCIMConfigConflict
		}
		client.CreatedAt = existing.CreatedAt
		client.Revision = existing.Revision + 1
		client.UpdatedAt = now
		client.UpdatedBy = strings.TrimSpace(input.Actor)
	case errors.Is(err, idb.ErrNotFound):
		if input.RequireRevisionSet {
			return nil, ErrSCIMConfigNotFound
		}
		client.CreatedAt = now
		client.UpdatedAt = now
		client.UpdatedBy = strings.TrimSpace(input.Actor)
		client.Revision = 1
	default:
		return nil, fmt.Errorf("SCIM config write: load current: %w", err)
	}

	seenSecretIDs := map[string]struct{}{}
	for i := range input.Secrets {
		secret := &input.Secrets[i]
		if strings.TrimSpace(secret.ClientID) != client.ID {
			return nil, fmt.Errorf("SCIM secret client id must match the client")
		}
		credentialID := strings.TrimSpace(secret.CredentialID)
		if credentialID == "" {
			return nil, fmt.Errorf("SCIM credential id is required")
		}
		if len(secret.Ciphertext) == 0 {
			return nil, fmt.Errorf("SCIM credential ciphertext is required")
		}
		if _, duplicate := seenSecretIDs[credentialID]; duplicate {
			return nil, fmt.Errorf("SCIM credential id %q duplicates another credential", credentialID)
		}
		seenSecretIDs[credentialID] = struct{}{}
		secret.Revision = client.Revision
		secret.UpdatedAt = now
		secret.UpdatedBy = strings.TrimSpace(input.Actor)
		if err := secrets.Put(ctx, scimSecretRecord(*secret)); err != nil {
			return nil, fmt.Errorf("SCIM secret write: %w", err)
		}
	}
	// Remove credentials no longer present in the submitted client. This makes
	// a one-to-two rotation and credential removal revoke old bearer tokens
	// atomically with the client update.
	existingRecords, err := secrets.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("SCIM secret write: load existing: %w", err)
	}
	for _, record := range existingRecords {
		secret := recordToSCIMSecret(record)
		if secret == nil || secret.ClientID != client.ID {
			continue
		}
		if _, keep := seenSecretIDs[secret.CredentialID]; !keep {
			if err := secrets.Delete(ctx, scimSecretKey(secret.ClientID, secret.CredentialID)); err != nil {
				return nil, fmt.Errorf("SCIM secret delete: %w", err)
			}
		}
	}
	if client.Enabled && len(input.Secrets) == 0 {
		return nil, fmt.Errorf("enabled SCIM client requires at least one credential")
	}
	if err := clients.Put(ctx, scimClientRecord(client)); err != nil {
		return nil, fmt.Errorf("SCIM config write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("SCIM config write: commit: %w", err)
	}
	committed = true
	return &client, nil
}

// Secrets returns encrypted credentials for one client. It never returns
// plaintext because this service does not possess the encryption key.
func (s *SCIMConfigService) Secrets(ctx context.Context, clientID string) ([]SCIMClientSecret, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("SCIM config service is not configured")
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, fmt.Errorf("SCIM client id is required")
	}
	store := s.db.ObjectStore(StoreSCIMSecrets)
	allRecords, err := store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list SCIM secrets: %w", err)
	}
	out := make([]SCIMClientSecret, 0, len(allRecords))
	for _, record := range allRecords {
		secret := recordToSCIMSecret(record)
		if secret != nil && secret.ClientID == clientID {
			out = append(out, *secret)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CredentialID < out[j].CredentialID })
	return out, nil
}

func scimClientRecord(client SCIMClientRecord) idb.Record {
	return idb.Record{
		"id":                       client.ID,
		"client_id":                client.ID,
		"authoritativeUserDomains": client.AuthoritativeUserDomains,
		"activeUserRelationships":  scimRelationshipRecords(client.ActiveUserRelationships),
		"enabled":                  client.Enabled,
		"retained":                 client.Retained,
		"created_at":               client.CreatedAt,
		"updated_at":               client.UpdatedAt,
		"updated_by":               client.UpdatedBy,
		"revision":                 client.Revision,
	}
}

func scimRelationshipRecords(rels []SCIMRelationshipData) []map[string]string {
	out := make([]map[string]string, len(rels))
	for i, rel := range rels {
		out[i] = map[string]string{
			"relation":     rel.Relation,
			"resourceType": rel.ResourceType,
			"resourceID":   rel.ResourceID,
		}
	}
	return out
}

func recordToSCIMClient(rec idb.Record) *SCIMClientRecord {
	if rec == nil {
		return nil
	}
	client := &SCIMClientRecord{
		ID:        strings.TrimSpace(recString(rec, "client_id")),
		Enabled:   recBool(rec, "enabled"),
		Retained:  recBool(rec, "retained"),
		CreatedAt: recTime(rec, "created_at"),
		UpdatedAt: recTime(rec, "updated_at"),
		UpdatedBy: recString(rec, "updated_by"),
		Revision:  int64(recUint64(rec, "revision")),
	}
	if client.ID == "" {
		return nil
	}
	client.AuthoritativeUserDomains = recStrings(rec, "authoritativeUserDomains")
	for _, item := range recAnySlice(rec, "activeUserRelationships") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		client.ActiveUserRelationships = append(client.ActiveUserRelationships, SCIMRelationshipData{
			Relation:     strings.TrimSpace(recString(m, "relation")),
			ResourceType: strings.TrimSpace(recString(m, "resourceType")),
			ResourceID:   strings.TrimSpace(recString(m, "resourceID")),
		})
	}
	return client
}

func scimSecretKey(clientID, credentialID string) string { return clientID + "\x00" + credentialID }

func scimSecretRecord(secret SCIMClientSecret) idb.Record {
	return idb.Record{
		"id":            secret.ClientID + "\x00" + secret.CredentialID,
		"client_id":     secret.ClientID,
		"credential_id": secret.CredentialID,
		"ciphertext":    secret.Ciphertext,
		"revision":      secret.Revision,
		"created_at":    secret.CreatedAt,
		"updated_at":    secret.UpdatedAt,
		"updated_by":    secret.UpdatedBy,
	}
}

func recordToSCIMSecret(rec idb.Record) *SCIMClientSecret {
	if rec == nil {
		return nil
	}
	secret := &SCIMClientSecret{
		ClientID:     strings.TrimSpace(recString(rec, "client_id")),
		CredentialID: strings.TrimSpace(recString(rec, "credential_id")),
		Ciphertext:   recBytes(rec, "ciphertext"),
		Revision:     int64(recUint64(rec, "revision")),
		CreatedAt:    recTime(rec, "created_at"),
		UpdatedAt:    recTime(rec, "updated_at"),
		UpdatedBy:    recString(rec, "updated_by"),
	}
	if secret.ClientID == "" || secret.CredentialID == "" || len(secret.Ciphertext) == 0 {
		return nil
	}
	return secret
}
