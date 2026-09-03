package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

// AppAllowedOperationsOverlay is the runtime delta app administrators manage
// without a deploy PR.
type AppAllowedOperationsOverlay struct {
	App        string
	Operations map[string]*operationexposure.OperationOverride
	Removed    []string
	UpdatedAt  time.Time
}

type AppAllowedOperationsService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppAllowedOperationsService(ds indexeddb.IndexedDB) *AppAllowedOperationsService {
	return &AppAllowedOperationsService{
		db:    ds,
		store: ds.ObjectStore(StoreAppAllowedOperations),
	}
}

func (s *AppAllowedOperationsService) EnsureStore(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("ensure app allowed operations store: service is not configured")
	}
	return ensureAppAllowedOperationsStore(ctx, s.db)
}

func (s *AppAllowedOperationsService) GetOverlay(ctx context.Context, app string) (*AppAllowedOperationsOverlay, error) {
	if s == nil {
		return nil, fmt.Errorf("get app allowed operations overlay: service is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return nil, fmt.Errorf("get app allowed operations overlay: app is required")
	}
	rec, err := s.store.Get(ctx, app)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app allowed operations overlay: %w", err)
	}
	return recordToAppAllowedOperationsOverlay(rec)
}

func (s *AppAllowedOperationsService) SetOverlay(ctx context.Context, overlay *AppAllowedOperationsOverlay) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("set app allowed operations overlay: service is not configured")
	}
	if overlay == nil {
		return fmt.Errorf("set app allowed operations overlay: overlay is required")
	}
	overlay.App = strings.TrimSpace(overlay.App)
	if overlay.App == "" {
		return fmt.Errorf("set app allowed operations overlay: app is required")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return err
	}
	overlay.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if err := s.store.Put(ctx, appAllowedOperationsRecord(overlay)); err != nil {
		return fmt.Errorf("set app allowed operations overlay: write: %w", err)
	}
	return nil
}

func (s *AppAllowedOperationsService) DeleteOverlay(ctx context.Context, app string) error {
	if s == nil {
		return fmt.Errorf("delete app allowed operations overlay: service is not configured")
	}
	app = strings.TrimSpace(app)
	if app == "" {
		return fmt.Errorf("delete app allowed operations overlay: app is required")
	}
	if err := s.EnsureStore(ctx); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, app); err != nil && !errors.Is(err, idb.ErrNotFound) {
		return fmt.Errorf("delete app allowed operations overlay: %w", err)
	}
	return nil
}

func appAllowedOperationsRecord(overlay *AppAllowedOperationsOverlay) idb.Record {
	operationsJSON, _ := json.Marshal(overlay.Operations)
	removedJSON, _ := json.Marshal(overlay.Removed)
	return idb.Record{
		"id":              overlay.App,
		"app":             overlay.App,
		"operations_json": string(operationsJSON),
		"removed_json":    string(removedJSON),
		"updated_at":      overlay.UpdatedAt,
	}
}

func recordToAppAllowedOperationsOverlay(rec idb.Record) (*AppAllowedOperationsOverlay, error) {
	operations := map[string]*operationexposure.OperationOverride{}
	if raw := recString(rec, "operations_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &operations); err != nil {
			return nil, fmt.Errorf("decode operations_json: %w", err)
		}
	}
	var removed []string
	if raw := recString(rec, "removed_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &removed); err != nil {
			return nil, fmt.Errorf("decode removed_json: %w", err)
		}
	}
	return &AppAllowedOperationsOverlay{
		App:        recString(rec, "app"),
		Operations: operations,
		Removed:    removed,
		UpdatedAt:  recTime(rec, "updated_at"),
	}, nil
}
