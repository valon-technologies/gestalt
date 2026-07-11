package coredata

import (
	"context"
	"fmt"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

type AppVersionCatalogService struct {
	store idb.ObjectStore
}

func NewAppVersionCatalogService(ds indexeddb.IndexedDB) *AppVersionCatalogService {
	return &AppVersionCatalogService{store: ds.ObjectStore(StoreAppVersionCatalog)}
}

func (s *AppVersionCatalogService) AppendRecord(ctx context.Context, record *core.AppVersionCatalogRecord) (*core.AppVersionCatalogRecord, error) {
	if record == nil {
		return nil, fmt.Errorf("append app version catalog record: record is required")
	}
	appName := strings.TrimSpace(record.App)
	if appName == "" {
		return nil, fmt.Errorf("append app version catalog record: app is required")
	}
	version := strings.TrimSpace(record.Version)
	if version == "" {
		return nil, fmt.Errorf("append app version catalog record: version is required")
	}
	recordType := strings.TrimSpace(record.Type)
	if recordType == "" {
		return nil, fmt.Errorf("append app version catalog record: type is required")
	}
	if err := validateAppVersionCatalogRecordType(recordType); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	id := strings.TrimSpace(record.ID)
	if id == "" {
		id = uuid.NewString()
	}
	timestamp := record.Timestamp
	if timestamp.IsZero() {
		timestamp = now
	} else {
		timestamp = timestamp.UTC().Truncate(time.Millisecond)
	}
	rec := idb.Record{
		"id":            id,
		"app":           appName,
		"version":       version,
		"type":          recordType,
		"actor":         strings.TrimSpace(record.Actor),
		"timestamp":     timestamp,
		"metadata_json": jsonValue(record.Metadata),
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return nil, fmt.Errorf("append app version catalog record: %w", err)
	}
	return recordToAppVersionCatalogRecord(rec), nil
}

var indexedDBMaxTime = time.Date(2262, 1, 1, 0, 0, 0, 0, time.UTC)

func (s *AppVersionCatalogService) ListRecordsByApp(ctx context.Context, appName string) ([]*core.AppVersionCatalogRecord, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("list app version catalog records: app is required")
	}
	query := idb.Bound(
		[]any{appName, time.Time{}},
		[]any{appName, indexedDBMaxTime},
		false,
		false,
	)
	recs, err := s.store.Index("by_app_timestamp").GetAll(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list app version catalog records: %w", err)
	}
	out := make([]*core.AppVersionCatalogRecord, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppVersionCatalogRecord(rec))
	}
	return out, nil
}

func (s *AppVersionCatalogService) HasKnownVersion(ctx context.Context, appName, version string) (bool, error) {
	appName = strings.TrimSpace(appName)
	version = strings.TrimSpace(version)
	if appName == "" || version == "" {
		return false, fmt.Errorf("has known app version: app and version are required")
	}
	query := idb.Bound(
		[]any{appName, version},
		[]any{appName, version},
		false,
		false,
	)
	recs, err := s.store.Index("by_app_version").GetAll(ctx, query)
	if err != nil {
		return false, fmt.Errorf("has known app version: %w", err)
	}
	for _, rec := range recs {
		if strings.TrimSpace(recString(rec, "type")) == core.AppVersionCatalogRecordTypeVersionAdded {
			return true, nil
		}
	}
	return false, nil
}

func validateAppVersionCatalogRecordType(recordType string) error {
	switch recordType {
	case core.AppVersionCatalogRecordTypeVersionAdded,
		core.AppVersionCatalogRecordTypeInstallFailed:
		return nil
	default:
		return fmt.Errorf("append app version catalog record: unsupported type %q", recordType)
	}
}

func recordToAppVersionCatalogRecord(rec idb.Record) *core.AppVersionCatalogRecord {
	return &core.AppVersionCatalogRecord{
		ID:        recString(rec, "id"),
		App:       recString(rec, "app"),
		Version:   recString(rec, "version"),
		Type:      recString(rec, "type"),
		Actor:     recString(rec, "actor"),
		Timestamp: recTime(rec, "timestamp"),
		Metadata:  recAnyMap(rec, "metadata_json"),
	}
}
