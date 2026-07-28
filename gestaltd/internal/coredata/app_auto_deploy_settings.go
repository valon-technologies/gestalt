package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

	tx, err := s.db.Transaction(
		ctx,
		[]string{StoreAppAutoDeploySettings},
		idb.TransactionReadwrite,
		idb.TransactionOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("update app auto-deploy settings: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(context.WithoutCancel(ctx))
		}
	}()

	store := tx.ObjectStore(StoreAppAutoDeploySettings)
	settings := &core.AppAutoDeploySettings{App: app}
	rec, err := store.Get(ctx, app)
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
	if err := store.Put(ctx, appAutoDeploySettingsRecord(settings)); err != nil {
		return nil, fmt.Errorf("update app auto-deploy settings: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update app auto-deploy settings: commit: %w", err)
	}
	committed = true
	return settings, nil
}

func normalizeAppAutoDeploySettings(settings *core.AppAutoDeploySettings) {
	settings.App = strings.TrimSpace(settings.App)
	settings.PendingVersion = strings.TrimSpace(settings.PendingVersion)
	settings.LastSeenVersion = strings.TrimSpace(settings.LastSeenVersion)
	settings.LastError = strings.TrimSpace(settings.LastError)
}

func appAutoDeploySettingsRecord(settings *core.AppAutoDeploySettings) idb.Record {
	return idb.Record{
		"id":                settings.App,
		"app":               settings.App,
		"enabled":           settings.Enabled,
		"pending_version":   settings.PendingVersion,
		"last_seen_version": settings.LastSeenVersion,
		"last_error":        settings.LastError,
	}
}

func recordToAppAutoDeploySettings(rec idb.Record) *core.AppAutoDeploySettings {
	enabled, _ := rec["enabled"].(bool)
	return &core.AppAutoDeploySettings{
		App:             recString(rec, "app"),
		Enabled:         enabled,
		PendingVersion:  recString(rec, "pending_version"),
		LastSeenVersion: recString(rec, "last_seen_version"),
		LastError:       recString(rec, "last_error"),
	}
}
