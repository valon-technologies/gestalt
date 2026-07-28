package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AutoDeploySettingsService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAutoDeploySettingsService(ds indexeddb.IndexedDB) *AutoDeploySettingsService {
	return &AutoDeploySettingsService{
		db:    ds,
		store: ds.ObjectStore(StoreAppAutoDeploySettings),
	}
}

// EnsureStore idempotently creates the app_auto_deploy_settings object store.
// Existing deployments may have started before the store was added to bootstrap.
func (s *AutoDeploySettingsService) EnsureStore(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("ensure app auto-deploy settings store: service is not configured")
	}
	return ensureAppAutoDeploySettingsStore(ctx, s.db)
}

func (s *AutoDeploySettingsService) Get(ctx context.Context, app string) (*core.AppAutoDeploySettings, error) {
	if s == nil {
		return nil, fmt.Errorf("get app auto-deploy settings: service is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("get app auto-deploy settings: app is required")
	}
	rec, err := s.store.Get(ctx, app)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app auto-deploy settings: %w", err)
	}
	return recordToAppAutoDeploySettings(rec), nil
}

func (s *AutoDeploySettingsService) ListEnabled(ctx context.Context) ([]*core.AppAutoDeploySettings, error) {
	if s == nil {
		return nil, fmt.Errorf("list enabled app auto-deploy settings: service is not configured")
	}
	recs, err := s.store.Index("by_enabled").GetAll(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("list enabled app auto-deploy settings: %w", err)
	}
	out := make([]*core.AppAutoDeploySettings, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppAutoDeploySettings(rec))
	}
	return out, nil
}

// Update atomically initializes or mutates one app's settings. A missing row
// starts disabled with all progress fields empty.
func (s *AutoDeploySettingsService) Update(
	ctx context.Context,
	app string,
	update func(*core.AppAutoDeploySettings) error,
) (*core.AppAutoDeploySettings, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("update app auto-deploy settings: service is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("update app auto-deploy settings: app is required")
	}
	if update == nil {
		return nil, fmt.Errorf("update app auto-deploy settings: update function is required")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return nil, err
	}

	settings := &core.AppAutoDeploySettings{App: app}
	rec, err := s.store.Get(ctx, app)
	if err == nil {
		settings = recordToAppAutoDeploySettings(rec)
	} else if !errors.Is(err, idb.ErrNotFound) {
		return nil, fmt.Errorf("update app auto-deploy settings: load current: %w", err)
	}
	if err := update(settings); err != nil {
		return nil, err
	}
	settings.App = app
	normalizeAppAutoDeploySettings(settings)
	if err := s.store.Put(ctx, appAutoDeploySettingsRecord(settings)); err != nil {
		return nil, fmt.Errorf("update app auto-deploy settings: write: %w", err)
	}
	return settings, nil
}

func normalizeAppAutoDeploySettings(settings *core.AppAutoDeploySettings) {
	settings.App = strings.TrimSpace(settings.App)
	settings.PendingVersion = strings.TrimSpace(settings.PendingVersion)
	settings.LastSeenVersion = strings.TrimSpace(settings.LastSeenVersion)
	settings.LastError = strings.TrimSpace(settings.LastError)
	if !settings.LastFailedRolloutAt.IsZero() {
		settings.LastFailedRolloutAt = settings.LastFailedRolloutAt.UTC().Truncate(time.Millisecond)
	}
}

func appAutoDeploySettingsRecord(settings *core.AppAutoDeploySettings) idb.Record {
	return idb.Record{
		"id":                     settings.App,
		"app":                    settings.App,
		"enabled":                settings.Enabled,
		"pending_version":        settings.PendingVersion,
		"last_seen_version":      settings.LastSeenVersion,
		"last_error":             settings.LastError,
		"last_failed_rollout_at": settings.LastFailedRolloutAt,
	}
}

func recordToAppAutoDeploySettings(rec idb.Record) *core.AppAutoDeploySettings {
	enabled, _ := rec["enabled"].(bool)
	return &core.AppAutoDeploySettings{
		App:                 recString(rec, "app"),
		Enabled:             enabled,
		PendingVersion:      recString(rec, "pending_version"),
		LastSeenVersion:     recString(rec, "last_seen_version"),
		LastError:           recString(rec, "last_error"),
		LastFailedRolloutAt: recTime(rec, "last_failed_rollout_at"),
	}
}
