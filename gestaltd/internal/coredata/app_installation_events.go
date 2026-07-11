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
	installationID := strings.TrimSpace(event.InstallationID)
	if installationID == "" {
		return nil, fmt.Errorf("append app installation event: installation_id is required")
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return nil, fmt.Errorf("append app installation event: type is required")
	}
	if err := validateAppInstallationEventType(eventType); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	id := strings.TrimSpace(event.ID)
	if id == "" {
		id = uuid.NewString()
	}
	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = now
	} else {
		timestamp = timestamp.UTC().Truncate(time.Millisecond)
	}
	rec := idb.Record{
		"id":                  id,
		"installation_id":     installationID,
		"from_version":        strings.TrimSpace(event.FromVersion),
		"to_version":          strings.TrimSpace(event.ToVersion),
		"type":                eventType,
		"actor":               strings.TrimSpace(event.Actor),
		"timestamp":           timestamp,
		"supersedes_event_id": strings.TrimSpace(event.SupersedesEventID),
		"metadata_json":       jsonValue(event.Metadata),
	}
	if err := s.store.Add(ctx, rec); err != nil {
		return nil, fmt.Errorf("append app installation event: %w", err)
	}
	return recordToAppInstallationEvent(rec), nil
}

var indexedDBMaxTime = time.Date(2262, 1, 1, 0, 0, 0, 0, time.UTC)

func (s *AppInstallationEventService) ListEventsByApp(ctx context.Context, installationID string) ([]*core.AppInstallationEvent, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return nil, fmt.Errorf("list app installation events: installation_id is required")
	}
	query := idb.Bound(
		[]any{installationID, time.Time{}},
		[]any{installationID, indexedDBMaxTime},
		false,
		false,
	)
	recs, err := s.store.Index("by_installation_id_timestamp").GetAll(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list app installation events: %w", err)
	}
	out := make([]*core.AppInstallationEvent, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppInstallationEvent(rec))
	}
	return out, nil
}

func validateAppInstallationEventType(eventType string) error {
	switch eventType {
	case core.AppInstallationEventTypeInstallRequested,
		core.AppInstallationEventTypePromoted,
		core.AppInstallationEventTypeFailed,
		core.AppInstallationEventTypeRollback,
		core.AppInstallationEventTypeUninstallRequested:
		return nil
	default:
		return fmt.Errorf("append app installation event: unsupported type %q", eventType)
	}
}

func recordToAppInstallationEvent(rec idb.Record) *core.AppInstallationEvent {
	return &core.AppInstallationEvent{
		ID:                recString(rec, "id"),
		InstallationID:    recString(rec, "installation_id"),
		FromVersion:       recString(rec, "from_version"),
		ToVersion:         recString(rec, "to_version"),
		Type:              recString(rec, "type"),
		Actor:             recString(rec, "actor"),
		Timestamp:         recTime(rec, "timestamp"),
		SupersedesEventID: recString(rec, "supersedes_event_id"),
		Metadata:          recAnyMap(rec, "metadata_json"),
	}
}
