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

const SCIMConfigSingletonID = "active"

// SCIMConfigRecord is one runtime-managed inbound SCIM client. Tokens are
// opaque to this layer and must be encrypted by the admin service before Put.
type SCIMConfigRecord struct {
	ID                       string
	Credentials              []SCIMCredentialRecord
	AuthoritativeUserDomains []string
	ActiveUserRelationships  []SCIMRelationshipRecord
	Enabled                  bool
	Retained                 bool
	CreatedAt                time.Time
	UpdatedAt                time.Time
	UpdatedBy                string
	Revision                 int64
}

type SCIMCredentialRecord struct {
	ID       string
	TokenRef string
}

type SCIMRelationshipRecord struct {
	Relation     string
	ResourceType string
	ResourceID   string
}

type SCIMConfigService struct {
	store idb.ObjectStore
}

func NewSCIMConfigService(ds indexeddb.IndexedDB) *SCIMConfigService {
	return &SCIMConfigService{store: ds.ObjectStore(StoreSCIMConfig)}
}

func (s *SCIMConfigService) EnsureStore(ctx context.Context) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("SCIM config service is not configured")
	}
	return nil
}

// List returns enabled and retained clients in stable client-ID order.
func (s *SCIMConfigService) List(ctx context.Context) ([]*SCIMConfigRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("SCIM config service is not configured")
	}
	records, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list SCIM config: %w", err)
	}
	out := make([]*SCIMConfigRecord, 0, len(records))
	for _, record := range records {
		client := recordToSCIMConfig(record)
		if client != nil {
			out = append(out, client)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *SCIMConfigService) Get(ctx context.Context, clientID string) (*SCIMConfigRecord, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("SCIM config service is not configured")
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, fmt.Errorf("SCIM client id is required")
	}
	rec, err := s.store.Get(ctx, scimConfigKey(clientID))
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, ErrSCIMConfigNotFound
		}
		return nil, fmt.Errorf("get SCIM config: %w", err)
	}
	client := recordToSCIMConfig(rec)
	if client == nil {
		return nil, ErrSCIMConfigNotFound
	}
	return client, nil
}

type PutSCIMConfigInput struct {
	Client             *SCIMConfigRecord
	Actor              string
	RequireRevision    int64
	RequireRevisionSet bool
}

func (s *SCIMConfigService) Put(ctx context.Context, input PutSCIMConfigInput) (*SCIMConfigRecord, error) {
	if s == nil || s.store == nil {
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
	now := time.Now().UTC()
	existingRec, err := s.store.Get(ctx, scimConfigKey(client.ID))
	switch {
	case err == nil:
		existing := recordToSCIMConfig(existingRec)
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
		if err := s.store.Put(ctx, scimConfigRecord(client)); err != nil {
			return nil, fmt.Errorf("put SCIM config: %w", err)
		}
		return &client, nil
	case errors.Is(err, idb.ErrNotFound):
		if input.RequireRevisionSet {
			return nil, ErrSCIMConfigNotFound
		}
		client.CreatedAt = now
		client.UpdatedAt = now
		client.UpdatedBy = strings.TrimSpace(input.Actor)
		client.Revision = 1
		if err := s.store.Add(ctx, scimConfigRecord(client)); err != nil {
			return nil, fmt.Errorf("put SCIM config: %w", err)
		}
		return &client, nil
	default:
		return nil, fmt.Errorf("put SCIM config: %w", err)
	}
}

func (s *SCIMConfigService) Delete(ctx context.Context, clientID string, actor string) (*SCIMConfigRecord, error) {
	existing, err := s.Get(ctx, clientID)
	if err != nil {
		return nil, err
	}
	existing.Enabled = false
	existing.Retained = true
	return s.Put(ctx, PutSCIMConfigInput{Client: existing, Actor: actor, RequireRevisionSet: true, RequireRevision: existing.Revision})
}

func scimConfigKey(clientID string) string { return "client\x00" + clientID }

func scimConfigRecord(client SCIMConfigRecord) idb.Record {
	credentials := make([]any, 0, len(client.Credentials))
	for _, credential := range client.Credentials {
		credentials = append(credentials, map[string]any{"id": credential.ID, "tokenRef": credential.TokenRef})
	}
	relationships := make([]any, 0, len(client.ActiveUserRelationships))
	for _, projection := range client.ActiveUserRelationships {
		relationships = append(relationships, map[string]any{
			"relation":     projection.Relation,
			"resourceType": projection.ResourceType,
			"resourceID":   projection.ResourceID,
		})
	}
	return idb.Record{
		"id":                       scimConfigKey(client.ID),
		"client_id":                client.ID,
		"credentials":              credentials,
		"authoritativeUserDomains": client.AuthoritativeUserDomains,
		"activeUserRelationships":  relationships,
		"enabled":                  client.Enabled,
		"retained":                 client.Retained,
		"created_at":               client.CreatedAt,
		"updated_at":               client.UpdatedAt,
		"updated_by":               client.UpdatedBy,
		"revision":                 client.Revision,
	}
}

func recordToSCIMConfig(rec idb.Record) *SCIMConfigRecord {
	if rec == nil {
		return nil
	}
	client := &SCIMConfigRecord{
		ID:                       strings.TrimSpace(recString(rec, "client_id")),
		AuthoritativeUserDomains: recStringList(rec, "authoritativeUserDomains"),
		Enabled:                  recBool(rec, "enabled"),
		Retained:                 recBool(rec, "retained"),
		CreatedAt:                recTime(rec, "created_at"),
		UpdatedAt:                recTime(rec, "updated_at"),
		UpdatedBy:                recString(rec, "updated_by"),
		Revision:                 recInt64Value(rec, "revision"),
	}
	for _, item := range recAnyList(rec, "credentials") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		client.Credentials = append(client.Credentials, SCIMCredentialRecord{
			ID:       strings.TrimSpace(stringValue(m["id"])),
			TokenRef: strings.TrimSpace(stringValue(m["tokenRef"])),
		})
	}
	for _, item := range recAnyList(rec, "activeUserRelationships") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		client.ActiveUserRelationships = append(client.ActiveUserRelationships, SCIMRelationshipRecord{
			Relation:     strings.TrimSpace(stringValue(m["relation"])),
			ResourceType: strings.TrimSpace(stringValue(m["resourceType"])),
			ResourceID:   strings.TrimSpace(stringValue(m["resourceID"])),
		})
	}
	if client.ID == "" {
		return nil
	}
	return client
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func recStringList(rec idb.Record, key string) []string {
	var out []string
	for _, item := range recAnyList(rec, key) {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func recAnyList(rec idb.Record, key string) []any {
	raw, ok := rec[key].([]any)
	if !ok {
		return nil
	}
	return raw
}

func recInt64Value(rec idb.Record, key string) int64 {
	switch value := rec[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}
