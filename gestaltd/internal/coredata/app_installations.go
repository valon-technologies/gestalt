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

// ErrInstallationStateConflict indicates the stored installation row changed
// before a compare-and-swap update could commit.
var ErrInstallationStateConflict = errors.New("app installation state conflict")

type AppInstallationService struct {
	db    indexeddb.IndexedDB
	store idb.ObjectStore
}

func NewAppInstallationService(ds indexeddb.IndexedDB) *AppInstallationService {
	return &AppInstallationService{
		db:    ds,
		store: ds.ObjectStore(StoreAppInstallations),
	}
}

func (s *AppInstallationService) GetInstallation(ctx context.Context, appName string) (*core.AppInstallation, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("get app installation: app_name is required")
	}
	rec, err := s.store.Get(ctx, appName)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("get app installation: %w", err)
	}
	return recordToAppInstallation(rec), nil
}

func (s *AppInstallationService) ListInstallations(ctx context.Context, rolloutStatus string) ([]*core.AppInstallation, error) {
	rolloutStatus = strings.TrimSpace(rolloutStatus)
	if rolloutStatus != "" {
		if err := validateAppInstallationRolloutStatus(rolloutStatus); err != nil {
			return nil, fmt.Errorf("list app installations: %w", err)
		}
	}
	var (
		recs []idb.Record
		err  error
	)
	if rolloutStatus == "" {
		recs, err = s.store.GetAll(ctx, nil)
	} else {
		recs, err = s.store.Index("by_rollout_status").GetAll(ctx, rolloutStatus)
	}
	if err != nil {
		return nil, fmt.Errorf("list app installations: %w", err)
	}
	out := make([]*core.AppInstallation, 0, len(recs))
	for _, rec := range recs {
		out = append(out, recordToAppInstallation(rec))
	}
	return out, nil
}

func (s *AppInstallationService) PutInstallation(ctx context.Context, installation *core.AppInstallation) (*core.AppInstallation, error) {
	if installation == nil {
		return nil, fmt.Errorf("put app installation: installation is required")
	}
	appName := strings.TrimSpace(installation.AppName)
	if appName == "" {
		return nil, fmt.Errorf("put app installation: app_name is required")
	}
	rolloutStatus := strings.TrimSpace(installation.RolloutStatus)
	if rolloutStatus == "" {
		return nil, fmt.Errorf("put app installation: rollout_status is required")
	}
	if err := validateAppInstallationRolloutStatus(rolloutStatus); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	existingRec, err := s.store.Get(ctx, appName)
	if err != nil && !errors.Is(err, idb.ErrNotFound) {
		return nil, fmt.Errorf("put app installation: %w", err)
	}
	var existing *core.AppInstallation
	if err == nil {
		existing = recordToAppInstallation(existingRec)
	}
	toStore := mergeAppInstallation(existing, installation)
	if toStore.InstalledAt.IsZero() {
		toStore.InstalledAt = now
	}
	rec := appInstallationToRecord(toStore)
	rec["id"] = appName
	rec["rollout_status"] = rolloutStatus
	rec["installed_at"] = toStore.InstalledAt
	rec["updated_at"] = now
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("put app installation: %w", err)
	}
	return recordToAppInstallation(rec), nil
}

func (s *AppInstallationService) UpdateInstallation(ctx context.Context, appName string, update func(*core.AppInstallation) error) (*core.AppInstallation, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("update app installation: app_name is required")
	}
	if update == nil {
		return nil, fmt.Errorf("update app installation: update is required")
	}
	installation, err := s.GetInstallation(ctx, appName)
	if err != nil {
		return nil, err
	}
	if err := update(installation); err != nil {
		return nil, err
	}
	rolloutStatus := strings.TrimSpace(installation.RolloutStatus)
	if rolloutStatus == "" {
		return nil, fmt.Errorf("update app installation: rollout_status is required")
	}
	if err := validateAppInstallationRolloutStatus(rolloutStatus); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	rec := appInstallationToRecord(installation)
	rec["id"] = appName
	rec["rollout_status"] = rolloutStatus
	rec["updated_at"] = now
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("update app installation: %w", err)
	}
	return recordToAppInstallation(rec), nil
}

func (s *AppInstallationService) CompareAndSwapInstallation(ctx context.Context, appName string, baseline *core.AppInstallation, update func(*core.AppInstallation) error) (*core.AppInstallation, error) {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return nil, fmt.Errorf("compare and swap app installation: app_name is required")
	}
	if update == nil {
		return nil, fmt.Errorf("compare and swap app installation: update is required")
	}

	cleanupCtx := context.WithoutCancel(ctx)
	tx, err := s.db.Transaction(cleanupCtx, []string{StoreAppInstallations}, idb.TransactionReadwrite, idb.TransactionOptions{})
	if err != nil {
		return nil, fmt.Errorf("compare and swap app installation: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Abort(cleanupCtx)
		}
	}()

	store := tx.ObjectStore(StoreAppInstallations)
	rec, getErr := store.Get(ctx, appName)
	var current *core.AppInstallation
	if getErr != nil {
		if !errors.Is(getErr, idb.ErrNotFound) {
			return nil, fmt.Errorf("compare and swap app installation: %w", getErr)
		}
		if baseline != nil {
			return nil, ErrInstallationStateConflict
		}
	} else {
		current = recordToAppInstallation(rec)
		if !InstallationMatchesBaseline(current, baseline) {
			return nil, ErrInstallationStateConflict
		}
	}

	installation := &core.AppInstallation{AppName: appName}
	if current != nil {
		*installation = *current
	}
	if err := update(installation); err != nil {
		return nil, err
	}

	rolloutStatus := strings.TrimSpace(installation.RolloutStatus)
	if rolloutStatus == "" {
		return nil, fmt.Errorf("compare and swap app installation: rollout_status is required")
	}
	if err := validateAppInstallationRolloutStatus(rolloutStatus); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	if installation.InstalledAt.IsZero() {
		installation.InstalledAt = now
	}
	installation.UpdatedAt = now

	outRec := appInstallationToRecord(installation)
	outRec["id"] = appName
	outRec["rollout_status"] = rolloutStatus
	outRec["installed_at"] = installation.InstalledAt
	outRec["updated_at"] = now
	if err := store.Put(ctx, outRec); err != nil {
		return nil, fmt.Errorf("compare and swap app installation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("compare and swap app installation: %w", err)
	}
	committed = true
	return recordToAppInstallation(outRec), nil
}

func (s *AppInstallationService) DeleteInstallation(ctx context.Context, appName string) error {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return fmt.Errorf("delete app installation: app_name is required")
	}
	if err := s.store.Delete(ctx, appName); err != nil {
		return fmt.Errorf("delete app installation: %w", err)
	}
	return nil
}

func InstallationMatchesBaseline(current, baseline *core.AppInstallation) bool {
	if baseline == nil {
		return current == nil
	}
	if current == nil {
		return false
	}
	return strings.TrimSpace(current.RolloutStatus) == strings.TrimSpace(baseline.RolloutStatus) &&
		strings.TrimSpace(current.ResolvedVersion) == strings.TrimSpace(baseline.ResolvedVersion) &&
		current.UpdatedAt.Equal(baseline.UpdatedAt)
}

func validateAppInstallationRolloutStatus(rolloutStatus string) error {
	switch rolloutStatus {
	case core.AppInstallationRolloutStatusPending,
		core.AppInstallationRolloutStatusPromoted,
		core.AppInstallationRolloutStatusFailed:
		return nil
	default:
		return fmt.Errorf("unsupported rollout_status %q", rolloutStatus)
	}
}

func mergeAppInstallation(existing *core.AppInstallation, incoming *core.AppInstallation) *core.AppInstallation {
	if existing == nil {
		out := *incoming
		return &out
	}
	out := *existing
	out.RolloutStatus = incoming.RolloutStatus
	if v := strings.TrimSpace(incoming.VersionConstraint); v != "" {
		out.VersionConstraint = v
	}
	if v := strings.TrimSpace(incoming.ResolvedVersion); v != "" {
		out.ResolvedVersion = v
	}
	if v := strings.TrimSpace(incoming.SourceRef); v != "" {
		out.SourceRef = v
	}
	if v := strings.TrimSpace(incoming.Registry); v != "" {
		out.Registry = v
	}
	if v := strings.TrimSpace(incoming.ProviderReleaseURL); v != "" {
		out.ProviderReleaseURL = v
	}
	if v := strings.TrimSpace(incoming.PreviousResolvedVersion); v != "" {
		out.PreviousResolvedVersion = v
	}
	if v := strings.TrimSpace(incoming.InstalledBy); v != "" {
		out.InstalledBy = v
	}
	if len(incoming.ArtifactChecksums) > 0 {
		out.ArtifactChecksums = incoming.ArtifactChecksums
	}
	if incoming.ActiveSince != nil {
		out.ActiveSince = incoming.ActiveSince
	}
	if !incoming.InstalledAt.IsZero() {
		out.InstalledAt = incoming.InstalledAt
	}
	return &out
}

func recordToAppInstallation(rec idb.Record) *core.AppInstallation {
	return &core.AppInstallation{
		AppName:                 recString(rec, "id"),
		VersionConstraint:       recString(rec, "version_constraint"),
		ResolvedVersion:         recString(rec, "resolved_version"),
		SourceRef:               recString(rec, "source_ref"),
		Registry:                recString(rec, "registry"),
		ProviderReleaseURL:      recString(rec, "provider_release_url"),
		ArtifactChecksums:       recStringMap(rec, "artifact_checksums_json"),
		RolloutStatus:           recString(rec, "rollout_status"),
		ActiveSince:             recTimePtr(rec, "active_since"),
		PreviousResolvedVersion: recString(rec, "previous_resolved_version"),
		InstalledBy:             recString(rec, "installed_by"),
		InstalledAt:             recTime(rec, "installed_at"),
		UpdatedAt:               recTime(rec, "updated_at"),
	}
}

func appInstallationToRecord(installation *core.AppInstallation) idb.Record {
	appName := strings.TrimSpace(installation.AppName)
	return idb.Record{
		"id":                        appName,
		"version_constraint":        strings.TrimSpace(installation.VersionConstraint),
		"resolved_version":          strings.TrimSpace(installation.ResolvedVersion),
		"source_ref":                strings.TrimSpace(installation.SourceRef),
		"registry":                  strings.TrimSpace(installation.Registry),
		"provider_release_url":      strings.TrimSpace(installation.ProviderReleaseURL),
		"artifact_checksums_json":   jsonValue(installation.ArtifactChecksums),
		"rollout_status":            strings.TrimSpace(installation.RolloutStatus),
		"active_since":              installation.ActiveSince,
		"previous_resolved_version": strings.TrimSpace(installation.PreviousResolvedVersion),
		"installed_by":              strings.TrimSpace(installation.InstalledBy),
		"installed_at":              installation.InstalledAt,
		"updated_at":                installation.UpdatedAt,
	}
}
