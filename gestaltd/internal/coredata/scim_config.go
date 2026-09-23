package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

var (
	ErrSCIMConfigNotFound = errors.New("SCIM client configuration not found")
	ErrSCIMConfigConflict = errors.New("SCIM client configuration was modified concurrently")
	ErrSCIMClientExists   = errors.New("SCIM client already exists")
)

// SCIMClientRecord is one runtime-managed inbound SCIM client. It embeds the
// canonical configuration type so runtime storage and config.yaml share one
// representation; the remaining fields are runtime ownership metadata.
type SCIMClientRecord struct {
	config.SCIMClientConfig
	ID        string
	Enabled   bool
	Retained  bool
	CreatedAt time.Time
	UpdatedAt time.Time
	UpdatedBy string
	Revision  int64
}

type SCIMConfigService struct {
	store idb.ObjectStore
}

func NewSCIMConfigService(ds indexeddb.IndexedDB) *SCIMConfigService {
	return &SCIMConfigService{store: ds.ObjectStore(StoreSCIMConfig)}
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

// Config returns enabled runtime clients in the canonical SCIM config shape.
func (s *SCIMConfigService) Config(ctx context.Context) (config.ServerSCIMConfig, bool, error) {
	clients, err := s.List(ctx)
	if err != nil {
		return config.ServerSCIMConfig{}, false, err
	}
	out := config.ServerSCIMConfig{Clients: make(map[string]config.SCIMClientConfig, len(clients))}
	for _, client := range clients {
		if client.Enabled {
			out.Clients[client.ID] = client.SCIMClientConfig
		}
	}
	return out, len(clients) > 0, nil
}

type PutSCIMClientInput struct {
	Client             *SCIMClientRecord
	Actor              string
	RequireRevision    int64
	RequireRevisionSet bool
}

func (s *SCIMConfigService) Put(ctx context.Context, input PutSCIMClientInput) (*SCIMClientRecord, error) {
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
	existingRec, err := s.store.Get(ctx, client.ID)
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
		if err := s.store.Put(ctx, scimClientRecord(client)); err != nil {
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
		if err := s.store.Add(ctx, scimClientRecord(client)); err != nil {
			return nil, fmt.Errorf("put SCIM config: %w", err)
		}
		return &client, nil
	default:
		return nil, fmt.Errorf("put SCIM config: %w", err)
	}
}

func scimClientRecord(client SCIMClientRecord) idb.Record {
	configJSON, err := json.Marshal(client.SCIMClientConfig)
	if err != nil {
		panic(fmt.Sprintf("marshal SCIM client config: %v", err))
	}
	return idb.Record{
		"id":          client.ID,
		"client_id":   client.ID,
		"config_json": string(configJSON),
		"enabled":     client.Enabled,
		"retained":    client.Retained,
		"created_at":  client.CreatedAt,
		"updated_at":  client.UpdatedAt,
		"updated_by":  client.UpdatedBy,
		"revision":    client.Revision,
	}
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
	if raw := recJSON(rec, "config_json"); len(raw) > 0 {
		if err := json.Unmarshal(raw, &client.SCIMClientConfig); err != nil {
			return nil
		}
	}
	return client
}
