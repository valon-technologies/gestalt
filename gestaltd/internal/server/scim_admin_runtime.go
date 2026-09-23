package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/scim"
)

var ErrSCIMRuntimeUnavailable = errors.New("SCIM runtime configuration is unavailable")

// SCIMRuntime manages runtime-owned SCIM configuration and applies it to the
// active HTTP handler atomically after validation.
type SCIMRuntime struct {
	services *coredata.Services
	db       indexeddb.IndexedDB
	authz    core.AuthorizationProvider
	runtime  *scim.Runtime
	baseURL  string
	fallback config.ServerSCIMConfig
	mu       sync.Mutex
}

func NewSCIMRuntime(services *coredata.Services, db indexeddb.IndexedDB, authz core.AuthorizationProvider, runtime *scim.Runtime, baseURL string, fallback config.ServerSCIMConfig) *SCIMRuntime {
	if runtime == nil {
		runtime = scim.NewRuntime(nil, config.ServerSCIMConfig{})
	}
	return &SCIMRuntime{services: services, db: db, authz: authz, runtime: runtime, baseURL: baseURL, fallback: fallback}
}

func (r *SCIMRuntime) available() error {
	if r == nil || r.services == nil || r.services.SCIMConfig == nil || r.db == nil {
		return ErrSCIMRuntimeUnavailable
	}
	return nil
}

// Current returns the active logical configuration. Disabled retained clients
// remain visible for audit and re-enable.
func (r *SCIMRuntime) Current(ctx context.Context) ([]*coredata.SCIMConfigRecord, string, error) {
	if err := r.available(); err != nil {
		return nil, "", err
	}
	clients, err := r.services.SCIMConfig.List(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(clients) == 0 {
		// Represent YAML compatibility as synthetic retained records without
		// persisting ownership on a read.
		for clientID, client := range r.fallback.Clients {
			record := recordFromConfig(clientID, client)
			record.Credentials = nil
			clients = append(clients, &record)
		}
	}
	source := "config"
	if len(clients) > 0 {
		source = "runtime"
	}
	return clients, source, nil
}

// Put validates and atomically stores one client, then applies the resulting
// configuration to the active SCIM runtime.
func (r *SCIMRuntime) Put(ctx context.Context, input *coredata.SCIMConfigRecord, actor string, requireRevisionSet bool, requireRevision int64) (*coredata.SCIMConfigRecord, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.ID) != input.ID {
		return nil, fmt.Errorf("SCIM client id must be non-empty and trimmed")
	}
	if !requireRevisionSet {
		if _, err := r.services.SCIMConfig.Get(ctx, input.ID); err == nil {
			return nil, coredata.ErrSCIMClientExists
		} else if !errors.Is(err, coredata.ErrSCIMConfigNotFound) {
			return nil, err
		}
	}
	if !input.Enabled && !input.Retained {
		// Disabled via PATCH still retains state; DELETE sets this explicitly.
		input.Retained = true
	}
	if input.Enabled {
		if err := r.validateLocked(ctx, *input); err != nil {
			return nil, err
		}
	}
	saved, err := r.services.SCIMConfig.Put(ctx, coredata.PutSCIMConfigInput{
		Client: input, Actor: actor, RequireRevisionSet: requireRevisionSet, RequireRevision: requireRevision,
	})
	if err != nil {
		return nil, err
	}
	if err := r.applyLocked(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *SCIMRuntime) Disable(ctx context.Context, clientID, actor string, revision int64) (*coredata.SCIMConfigRecord, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	current, err := r.services.SCIMConfig.Get(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if revision != 0 && current.Revision != revision {
		return nil, coredata.ErrSCIMConfigConflict
	}
	current.Enabled = false
	current.Retained = true
	return r.Put(ctx, current, actor, true, current.Revision)
}

func (r *SCIMRuntime) validateLocked(ctx context.Context, client coredata.SCIMConfigRecord) error {
	all, err := r.services.SCIMConfig.List(ctx)
	if err != nil {
		return err
	}
	cfg := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{}}
	for _, existing := range all {
		if !existing.Enabled || existing.ID == client.ID {
			continue
		}
		cfg.Clients[existing.ID] = configFromRecord(*existing)
	}
	cfg.Clients[client.ID] = configFromRecord(client)
	if r.authz == nil {
		for _, candidate := range cfg.Clients {
			if len(candidate.ActiveUserRelationships) > 0 || len(candidate.AuthoritativeUserDomains) > 0 {
				return fmt.Errorf("SCIM authorization provider is required when authoritative domains or activeUserRelationships are configured")
			}
		}
	}
	// NewService performs the full model-aware validation without mutating state.
	_, err = scim.NewService(r.db, r.authz, r.baseURL, cfg)
	return err
}

func (r *SCIMRuntime) applyLocked(ctx context.Context) error {
	clients, err := r.services.SCIMConfig.List(ctx)
	if err != nil {
		return err
	}
	cfg := config.ServerSCIMConfig{Clients: map[string]config.SCIMClientConfig{}}
	for _, client := range clients {
		if !client.Enabled {
			continue
		}
		cfg.Clients[client.ID] = configFromRecord(*client)
	}
	service, err := scim.NewService(r.db, r.authz, r.baseURL, cfg)
	if err != nil {
		return err
	}
	r.runtime.Apply(service, cfg)
	return nil
}

func configFromRecord(client coredata.SCIMConfigRecord) config.SCIMClientConfig {
	out := config.SCIMClientConfig{
		Credentials:              make([]config.SCIMCredentialConfig, 0, len(client.Credentials)),
		AuthoritativeUserDomains: client.AuthoritativeUserDomains,
		ActiveUserRelationships:  make([]config.SCIMRelationshipConfig, 0, len(client.ActiveUserRelationships)),
	}
	for _, credential := range client.Credentials {
		out.Credentials = append(out.Credentials, config.SCIMCredentialConfig{ID: credential.ID, BearerToken: credential.TokenRef})
	}
	for _, projection := range client.ActiveUserRelationships {
		out.ActiveUserRelationships = append(out.ActiveUserRelationships, config.SCIMRelationshipConfig{
			Relation: projection.Relation,
			Resource: config.AuthorizationResourceDef{Type: projection.ResourceType, ID: projection.ResourceID},
		})
	}
	return out
}

func recordFromConfig(clientID string, client config.SCIMClientConfig) coredata.SCIMConfigRecord {
	record := coredata.SCIMConfigRecord{
		ID:                       clientID,
		Enabled:                  true,
		AuthoritativeUserDomains: client.AuthoritativeUserDomains,
	}
	for _, credential := range client.Credentials {
		record.Credentials = append(record.Credentials, coredata.SCIMCredentialRecord{
			ID: credential.ID, TokenRef: credential.BearerToken,
		})
	}
	for _, projection := range client.ActiveUserRelationships {
		record.ActiveUserRelationships = append(record.ActiveUserRelationships, coredata.SCIMRelationshipRecord{
			Relation: projection.Relation, ResourceType: projection.Resource.Type, ResourceID: projection.Resource.ID,
		})
	}
	return record
}
