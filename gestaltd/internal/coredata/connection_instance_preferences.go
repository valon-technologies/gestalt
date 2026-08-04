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

const connectionInstancePreferenceKeySep = "\x1f"

type ConnectionInstancePreference struct {
	SubjectID    string
	ConnectionID string
	Instance     string
	UpdatedAt    time.Time
}

type ConnectionInstancePreferenceService struct {
	store idb.ObjectStore
}

func NewConnectionInstancePreferenceService(ds indexeddb.IndexedDB) *ConnectionInstancePreferenceService {
	return &ConnectionInstancePreferenceService{store: ds.ObjectStore(StoreConnectionInstancePreferences)}
}

func connectionInstancePreferenceKey(subjectID, connectionID string) string {
	return strings.TrimSpace(subjectID) + connectionInstancePreferenceKeySep + strings.TrimSpace(connectionID)
}

func (s *ConnectionInstancePreferenceService) Get(ctx context.Context, subjectID, connectionID string) (*ConnectionInstancePreference, error) {
	if s == nil {
		return nil, fmt.Errorf("get connection instance preference: service is not configured")
	}
	subjectID = strings.TrimSpace(subjectID)
	connectionID = strings.TrimSpace(connectionID)
	if subjectID == "" || connectionID == "" {
		return nil, fmt.Errorf("get connection instance preference: subject_id and connection_id are required")
	}
	rec, err := s.store.Get(ctx, connectionInstancePreferenceKey(subjectID, connectionID))
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get connection instance preference: %w", err)
	}
	return recordToConnectionInstancePreference(rec), nil
}

func (s *ConnectionInstancePreferenceService) Set(ctx context.Context, subjectID, connectionID, instance string) (*ConnectionInstancePreference, error) {
	if s == nil {
		return nil, fmt.Errorf("set connection instance preference: service is not configured")
	}
	subjectID = strings.TrimSpace(subjectID)
	connectionID = strings.TrimSpace(connectionID)
	instance = strings.TrimSpace(instance)
	if subjectID == "" || connectionID == "" {
		return nil, fmt.Errorf("set connection instance preference: subject_id and connection_id are required")
	}
	if instance == "" {
		return nil, fmt.Errorf("set connection instance preference: instance is required")
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	pref := &ConnectionInstancePreference{
		SubjectID:    subjectID,
		ConnectionID: connectionID,
		Instance:     instance,
		UpdatedAt:    now,
	}
	if err := s.store.Put(ctx, connectionInstancePreferenceRecord(pref)); err != nil {
		return nil, fmt.Errorf("set connection instance preference: %w", err)
	}
	return pref, nil
}

func (s *ConnectionInstancePreferenceService) PreferredInstance(ctx context.Context, subjectID, connectionID string) (string, error) {
	pref, err := s.Get(ctx, subjectID, connectionID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	if pref == nil {
		return "", nil
	}
	return pref.Instance, nil
}

func (s *ConnectionInstancePreferenceService) Delete(ctx context.Context, subjectID, connectionID string) error {
	if s == nil {
		return fmt.Errorf("delete connection instance preference: service is not configured")
	}
	subjectID = strings.TrimSpace(subjectID)
	connectionID = strings.TrimSpace(connectionID)
	if subjectID == "" || connectionID == "" {
		return fmt.Errorf("delete connection instance preference: subject_id and connection_id are required")
	}
	if err := s.store.Delete(ctx, connectionInstancePreferenceKey(subjectID, connectionID)); err != nil {
		return fmt.Errorf("delete connection instance preference: %w", err)
	}
	return nil
}

func connectionInstancePreferenceRecord(pref *ConnectionInstancePreference) idb.Record {
	return idb.Record{
		"id":            connectionInstancePreferenceKey(pref.SubjectID, pref.ConnectionID),
		"subject_id":    pref.SubjectID,
		"connection_id": pref.ConnectionID,
		"instance":      pref.Instance,
		"updated_at":    pref.UpdatedAt,
	}
}

func recordToConnectionInstancePreference(rec idb.Record) *ConnectionInstancePreference {
	return &ConnectionInstancePreference{
		SubjectID:    recString(rec, "subject_id"),
		ConnectionID: recString(rec, "connection_id"),
		Instance:     recString(rec, "instance"),
		UpdatedAt:    recTime(rec, "updated_at"),
	}
}
