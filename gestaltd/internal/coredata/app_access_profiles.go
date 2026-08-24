package coredata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/indexeddb"
)

const appAccessProfileKeySep = "\x1f"

// AppAccessProfileService is the durable store for user-selected app
// capabilities. Profiles are keyed by the canonical credential subject and
// app, so a user's settings follow the user across connection methods and
// account instances.
type AppAccessProfileService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppAccessProfileService(ds indexeddb.IndexedDB) *AppAccessProfileService {
	return &AppAccessProfileService{
		db:    ds,
		store: ds.ObjectStore(StoreAppAccessProfiles),
	}
}

// EnsureStore idempotently creates the profile store for deployments that
// started before app capabilities were introduced.
func (s *AppAccessProfileService) EnsureStore(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("ensure app access profiles store: service is not configured")
	}
	return ensureAppAccessProfilesStore(ctx, s.db)
}

func (s *AppAccessProfileService) GetAppAccessProfile(ctx context.Context, subjectID, app string) (*core.AppAccessProfile, error) {
	if s == nil {
		return nil, fmt.Errorf("get app access profile: service is not configured")
	}
	key, subjectID, app, err := validateAppAccessProfileKey(subjectID, app)
	if err != nil {
		return nil, err
	}
	rec, err := s.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app access profile: %w", err)
	}
	profile, err := recordToAppAccessProfile(rec)
	if err != nil {
		return nil, fmt.Errorf("get app access profile: %w", err)
	}
	profile.SubjectID = subjectID
	profile.App = app
	return profile, nil
}

// EnsureAppAccessDefaults creates the initial profile only when it does not
// already exist. Existing profiles are returned unchanged, including an
// intentionally empty allow list.
func (s *AppAccessProfileService) EnsureAppAccessDefaults(ctx context.Context, subjectID, app string, operations []string) (*core.AppAccessProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("ensure app access defaults: service is not configured")
	}
	_, subjectID, app, err := validateAppAccessProfileKey(subjectID, app)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureStore(ctx); err != nil {
		return nil, err
	}
	profile := &core.AppAccessProfile{
		SubjectID:           subjectID,
		App:                 app,
		EnabledOperations:   normalizeAppAccessOperations(operations),
		DefaultsInitialized: true,
		UpdatedAt:           time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.store.Add(ctx, appAccessProfileRecord(profile)); err == nil {
		return profile, nil
	} else if !errors.Is(err, idb.ErrAlreadyExists) {
		return nil, fmt.Errorf("ensure app access defaults: write: %w", err)
	}
	// Another connection completion won the create race. Read its profile and
	// preserve the user's existing choices rather than replacing them.
	existing, err := s.GetAppAccessProfile(ctx, subjectID, app)
	if err != nil {
		return nil, fmt.Errorf("ensure app access defaults: load current: %w", err)
	}
	return existing, nil
}

func (s *AppAccessProfileService) SetAppAccessOperations(ctx context.Context, subjectID, app string, operations []string) (*core.AppAccessProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("set app access operations: service is not configured")
	}
	_, subjectID, app, err := validateAppAccessProfileKey(subjectID, app)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureStore(ctx); err != nil {
		return nil, err
	}
	profile, err := s.GetAppAccessProfile(ctx, subjectID, app)
	if err != nil && !errors.Is(err, core.ErrNotFound) {
		return nil, err
	}
	if profile == nil {
		profile = &core.AppAccessProfile{SubjectID: subjectID, App: app}
	}
	profile.EnabledOperations = normalizeAppAccessOperations(operations)
	profile.DefaultsInitialized = true
	profile.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
	if err := s.store.Put(ctx, appAccessProfileRecord(profile)); err != nil {
		return nil, fmt.Errorf("set app access operations: write: %w", err)
	}
	return profile, nil
}

func validateAppAccessProfileKey(subjectID, app string) (string, string, string, error) {
	subjectID = strings.TrimSpace(subjectID)
	app = strings.TrimSpace(app)
	if subjectID == "" || app == "" {
		return "", "", "", fmt.Errorf("app access profile: subject_id and app are required")
	}
	return subjectID + appAccessProfileKeySep + app, subjectID, app, nil
}

func normalizeAppAccessOperations(operations []string) []string {
	if len(operations) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(operations))
	out := make([]string, 0, len(operations))
	for _, operation := range operations {
		operation = strings.TrimSpace(operation)
		if operation == "" {
			continue
		}
		if _, ok := seen[operation]; ok {
			continue
		}
		seen[operation] = struct{}{}
		out = append(out, operation)
	}
	slices.Sort(out)
	return out
}

func appAccessProfileRecord(profile *core.AppAccessProfile) idb.Record {
	operations, _ := json.Marshal(normalizeAppAccessOperations(profile.EnabledOperations))
	return idb.Record{
		"id":                   strings.TrimSpace(profile.SubjectID) + appAccessProfileKeySep + strings.TrimSpace(profile.App),
		"subject_id":           strings.TrimSpace(profile.SubjectID),
		"app":                  strings.TrimSpace(profile.App),
		"enabled_operations":   string(operations),
		"defaults_initialized": profile.DefaultsInitialized,
		"updated_at":           profile.UpdatedAt.UTC().Truncate(time.Millisecond),
	}
}

func recordToAppAccessProfile(rec idb.Record) (*core.AppAccessProfile, error) {
	var operations []string
	if raw := recString(rec, "enabled_operations"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &operations); err != nil {
			return nil, fmt.Errorf("decode enabled operations: %w", err)
		}
	}
	initialized, _ := rec["defaults_initialized"].(bool)
	return &core.AppAccessProfile{
		SubjectID:           recString(rec, "subject_id"),
		App:                 recString(rec, "app"),
		EnabledOperations:   normalizeAppAccessOperations(operations),
		DefaultsInitialized: initialized,
		UpdatedAt:           recTime(rec, "updated_at"),
	}, nil
}
