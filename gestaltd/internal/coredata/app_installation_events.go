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

type AppInstallationEventService struct {
	store idb.ObjectStore
}

func NewAppInstallationEventService(ds indexeddb.IndexedDB) *AppInstallationEventService {
	return &AppInstallationEventService{store: ds.ObjectStore(StoreAppInstallationEvents)}
}

func (s *AppInstallationEventService) AppendEvent(ctx context.Context, event *core.AppInstallationEvent) (*core.AppInstallationEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("append app installation event: event is required")
	}
	appName := strings.TrimSpace(event.AppName)
	if appName == "" {
		return nil, fmt.Errorf("append app installation event: app_name is required")
	}
	eventType := strings.TrimSpace(event.EventType)
	if eventType == "" {
		return nil, fmt.Errorf("append app installation event: event_type is required")
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	eventID := strings.TrimSpace(event.EventID)
	if eventID == "" {
		eventID = uuid.NewString()
	}
	rec := idb.Record{
		"id":           eventID,
		"event_id":     eventID,
		"app_name":     appName,
		"from_version": strings.TrimSpace(event.FromVersion),
		"to_version":   strings.TrimSpace(event.ToVersion),
		"event_type":   eventType,
		"actor":        strings.TrimSpace(event.Actor),
		"created_at":   now,
		"metadata_json": jsonValue(event.Metadata),
	}
	if !event.CreatedAt.IsZero() {
		rec["created_at"] = event.CreatedAt.UTC().Truncate(time.Millisecond)
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return nil, fmt.Errorf("append app installation event: %w", err)
	}
	return recordToAppInstallationEvent(rec), nil
}

func (s *AppInstallationEventService) ListEventsByApp(ctx context.Context, appName string) ([]*core.AppInstallationEvent, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("list app installation events: app_name is required")
	}
	recs, err := s.store.Index("by_app_name").GetAll(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("list app installation events: %w", err)
	}
	out := make([]*core.AppInstallationEvent, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppInstallationEvent(rec))
	}
	return out, nil
}

func recordToAppInstallationEvent(rec idb.Record) *core.AppInstallationEvent {
	eventID := recString(rec, "event_id")
	if eventID == "" {
		eventID = recString(rec, "id")
	}
	return &core.AppInstallationEvent{
		EventID:     eventID,
		AppName:     recString(rec, "app_name"),
		FromVersion: recString(rec, "from_version"),
		ToVersion:   recString(rec, "to_version"),
		EventType:   recString(rec, "event_type"),
		Actor:       recString(rec, "actor"),
		CreatedAt:   recTime(rec, "created_at"),
		Metadata:    recAnyMap(rec, "metadata_json"),
	}
}
