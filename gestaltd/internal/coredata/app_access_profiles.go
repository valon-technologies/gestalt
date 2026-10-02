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

// EnsureAppAccessDefaults creates an empty profile, meaning "follow the app's
// defaults", only when none exists. Existing profiles are returned unchanged,
// including every choice the user made.
func (s *AppAccessProfileService) EnsureAppAccessDefaults(ctx context.Context, subjectID, app string) (*core.AppAccessProfile, error) {
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

// SetAppAccessOverrides replaces the user's decisions. A save always writes
// the current format, so a legacy profile is converted the first time its
// owner saves.
func (s *AppAccessProfileService) SetAppAccessOverrides(ctx context.Context, subjectID, app string, disabled, extra []string) (*core.AppAccessProfile, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("set app access overrides: service is not configured")
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
		DisabledOperations:  normalizeAppAccessOperations(disabled),
		ExtraOperations:     normalizeAppAccessOperations(extra),
		DefaultsInitialized: true,
		UpdatedAt:           time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.store.Put(ctx, appAccessProfileRecord(profile)); err != nil {
		return nil, fmt.Errorf("set app access overrides: write: %w", err)
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

// appAccessPayloadVersion marks the decisions-relative-to-defaults format.
// Records written before it hold a bare JSON array of enabled operations in the
// same column, which recordToAppAccessProfile still reads, so no schema
// migration is needed.
//
// Rollback consequence: an older binary reading a new-format row fails to
// decode it and denies access until the fleet runs the new version. Deploys are
// zero-gap (no-traffic, then promote), so this skew window is short.
const appAccessPayloadVersion = 1

type appAccessPayload struct {
	Version  int      `json:"v"`
	Disabled []string `json:"disabled,omitempty"`
	Extra    []string `json:"extra,omitempty"`
}

func appAccessProfileRecord(profile *core.AppAccessProfile) idb.Record {
	payload, _ := json.Marshal(appAccessPayload{
		Version:  appAccessPayloadVersion,
		Disabled: normalizeAppAccessOperations(profile.DisabledOperations),
		Extra:    normalizeAppAccessOperations(profile.ExtraOperations),
	})
	return idb.Record{
		"id":                   strings.TrimSpace(profile.SubjectID) + appAccessProfileKeySep + strings.TrimSpace(profile.App),
		"subject_id":           strings.TrimSpace(profile.SubjectID),
		"app":                  strings.TrimSpace(profile.App),
		"enabled_operations":   string(payload),
		"defaults_initialized": profile.DefaultsInitialized,
		"updated_at":           profile.UpdatedAt.UTC().Truncate(time.Millisecond),
	}
}

func recordToAppAccessProfile(rec idb.Record) (*core.AppAccessProfile, error) {
	subjectID := recString(rec, "subject_id")
	app := recString(rec, "app")
	updatedAt := recTime(rec, "updated_at")
	raw := strings.TrimSpace(recString(rec, "enabled_operations"))
	var profile *core.AppAccessProfile
	if strings.HasPrefix(raw, "{") {
		var payload appAccessPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, fmt.Errorf("decode app access payload: %w", err)
		}
		if payload.Version != appAccessPayloadVersion {
			// An unknown version may carry decisions this build cannot read.
			// Failing closed keeps a rollback from silently widening access.
			return nil, fmt.Errorf("decode app access payload: unsupported version %d", payload.Version)
		}
		profile = core.NewRelativeAppAccessProfile(subjectID, app, payload.Disabled, payload.Extra, updatedAt)
	} else {
		var operations []string
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &operations); err != nil {
				return nil, fmt.Errorf("decode enabled operations: %w", err)
			}
		}
		profile = core.NewLegacyAppAccessProfile(subjectID, app, operations, updatedAt)
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	return profile, nil
}
