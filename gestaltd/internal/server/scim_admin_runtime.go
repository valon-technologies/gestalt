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

// Current returns retained runtime clients, or YAML clients when runtime
// storage is empty. Disabled retained clients remain visible for re-enable.
func (r *SCIMRuntime) Current(ctx context.Context) ([]*coredata.SCIMClientRecord, string, error) {
	if err := r.available(); err != nil {
		return nil, "", err
	}
	clients, err := r.services.SCIMConfig.List(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(clients) == 0 {
		for clientID, client := range r.fallback.Clients {
			clients = append(clients, &coredata.SCIMClientRecord{
				ID:               clientID,
				SCIMClientConfig: client,
				Enabled:          true,
			})
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
func (r *SCIMRuntime) Put(ctx context.Context, input *coredata.SCIMClientRecord, actor string, requireRevisionSet bool, requireRevision int64) (*coredata.SCIMClientRecord, error) {
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
	saved, err := r.services.SCIMConfig.Put(ctx, coredata.PutSCIMClientInput{
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

// Disable reuses Put so deletion has the same revision and audit semantics.
func (r *SCIMRuntime) Disable(ctx context.Context, clientID, actor string, revision int64) (*coredata.SCIMClientRecord, error) {
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

func (r *SCIMRuntime) validateLocked(ctx context.Context, client coredata.SCIMClientRecord) error {
	cfg, _, err := r.services.SCIMConfig.Config(ctx)
	if err != nil {
		return err
	}
	if cfg.Clients == nil {
		cfg.Clients = map[string]config.SCIMClientConfig{}
	}
	delete(cfg.Clients, client.ID)
	cfg.Clients[client.ID] = client.SCIMClientConfig
	_, err = scim.NewService(r.db, r.authz, r.baseURL, cfg)
	return err
}

func (r *SCIMRuntime) applyLocked(ctx context.Context) error {
	cfg, _, err := r.services.SCIMConfig.Config(ctx)
	if err != nil {
		return err
	}
	service, err := scim.NewService(r.db, r.authz, r.baseURL, cfg)
	if err != nil {
		return err
	}
	r.runtime.Apply(service, cfg)
	return nil
}
