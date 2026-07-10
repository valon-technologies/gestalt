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

type AppInstallationService struct {
	store idb.ObjectStore
}

func NewAppInstallationService(ds indexeddb.IndexedDB) *AppInstallationService {
	return &AppInstallationService{store: ds.ObjectStore(StoreAppInstallations)}
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
	rec := appInstallationToRecord(installation)
	rec["id"] = appName
	rec["app_name"] = appName
	rec["rollout_status"] = rolloutStatus
	if recTime(rec, "installed_at").IsZero() {
		rec["installed_at"] = now
	}
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
	rec["app_name"] = appName
	rec["rollout_status"] = rolloutStatus
	rec["updated_at"] = now
	if err := s.store.Put(ctx, rec); err != nil {
		return nil, fmt.Errorf("update app installation: %w", err)
	}
	return recordToAppInstallation(rec), nil
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

func validateAppInstallationRolloutStatus(rolloutStatus string) error {
	switch rolloutStatus {
	case core.AppInstallationRolloutStatusPending,
		core.AppInstallationRolloutStatusPromoted,
		core.AppInstallationRolloutStatusFailed:
		return nil
	default:
		return fmt.Errorf("put app installation: unsupported rollout_status %q", rolloutStatus)
	}
}

func recordToAppInstallation(rec idb.Record) *core.AppInstallation {
	appName := recString(rec, "app_name")
	if appName == "" {
		appName = recString(rec, "id")
	}
	return &core.AppInstallation{
		AppName:                 appName,
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
		"id":                          appName,
		"app_name":                    appName,
		"version_constraint":          strings.TrimSpace(installation.VersionConstraint),
		"resolved_version":            strings.TrimSpace(installation.ResolvedVersion),
		"source_ref":                  strings.TrimSpace(installation.SourceRef),
		"registry":                    strings.TrimSpace(installation.Registry),
		"provider_release_url":        strings.TrimSpace(installation.ProviderReleaseURL),
		"artifact_checksums_json":     jsonValue(installation.ArtifactChecksums),
		"rollout_status":              strings.TrimSpace(installation.RolloutStatus),
		"active_since":                installation.ActiveSince,
		"previous_resolved_version":   strings.TrimSpace(installation.PreviousResolvedVersion),
		"installed_by":                strings.TrimSpace(installation.InstalledBy),
		"installed_at":                installation.InstalledAt,
		"updated_at":                  installation.UpdatedAt,
	}
}
