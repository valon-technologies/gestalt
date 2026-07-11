package coredata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
)

const (
	appVersionCatalogMetaRegistry           = "registry"
	appVersionCatalogMetaMaterializedPath   = "materialized_path"
	appVersionCatalogMetaSourceRef          = "source_ref"
	appVersionCatalogMetaProviderReleaseURL = "provider_release_url"
	appVersionCatalogMetaArtifactChecksums  = "artifact_checksums"
	appVersionCatalogMetaInstalledAt        = "installed_at"
)

// VersionAddedMetadata builds the metadata payload for a version_added record.
func VersionAddedMetadata(installation *core.AppInstallation, materializedPath string) map[string]any {
	if installation == nil {
		return nil
	}
	metadata := map[string]any{
		appVersionCatalogMetaRegistry:           strings.TrimSpace(installation.Registry),
		appVersionCatalogMetaMaterializedPath:   strings.TrimSpace(materializedPath),
		appVersionCatalogMetaSourceRef:          strings.TrimSpace(installation.SourceRef),
		appVersionCatalogMetaProviderReleaseURL: strings.TrimSpace(installation.ProviderReleaseURL),
	}
	if len(installation.ArtifactChecksums) > 0 {
		metadata[appVersionCatalogMetaArtifactChecksums] = installation.ArtifactChecksums
	}
	if !installation.InstalledAt.IsZero() {
		metadata[appVersionCatalogMetaInstalledAt] = installation.InstalledAt.UTC().Format(time.RFC3339Nano)
	}
	return metadata
}

// ListKnownVersionsByApp returns known versions for one app from version_added records.
func (s *AppVersionCatalogService) ListKnownVersionsByApp(ctx context.Context, appName string) ([]*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("list known app versions: catalog service is not configured")
	}
	records, err := s.ListRecordsByApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	return knownVersionsFromRecords(records), nil
}

// ListAllKnownVersions returns known versions across all apps from version_added records.
func (s *AppVersionCatalogService) ListAllKnownVersions(ctx context.Context) ([]*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("list all known app versions: catalog service is not configured")
	}
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list all known app versions: %w", err)
	}
	records := make([]*core.AppVersionCatalogRecord, 0, len(recs))
	for _, rec := range recs {
		records = append(records, recordToAppVersionCatalogRecord(rec))
	}
	return knownVersionsFromRecords(records), nil
}

func knownVersionsFromRecords(records []*core.AppVersionCatalogRecord) []*core.AppInstallation {
	byAppVersion := make(map[string]*core.AppInstallation)
	for _, record := range records {
		if record == nil || strings.TrimSpace(record.Type) != core.AppVersionCatalogRecordTypeVersionAdded {
			continue
		}
		appName := strings.TrimSpace(record.App)
		version := strings.TrimSpace(record.Version)
		if appName == "" || version == "" {
			continue
		}
		installation := installationFromVersionAddedRecord(record)
		if installation == nil {
			continue
		}
		key := appName + "\x00" + version
		existing := byAppVersion[key]
		if existing == nil || installation.UpdatedAt.After(existing.UpdatedAt) {
			byAppVersion[key] = installation
		}
	}
	out := make([]*core.AppInstallation, 0, len(byAppVersion))
	for _, installation := range byAppVersion {
		out = append(out, installation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AppName != out[j].AppName {
			return out[i].AppName < out[j].AppName
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func installationFromVersionAddedRecord(record *core.AppVersionCatalogRecord) *core.AppInstallation {
	if record == nil {
		return nil
	}
	addedAt := record.Timestamp.UTC().Truncate(time.Millisecond)
	installation := &core.AppInstallation{
		AppName:     strings.TrimSpace(record.App),
		Version:     strings.TrimSpace(record.Version),
		InstalledBy: strings.TrimSpace(record.Actor),
		InstalledAt: addedAt,
		UpdatedAt:   addedAt,
	}
	if metadata := record.Metadata; metadata != nil {
		installation.Registry = stringMeta(metadata, appVersionCatalogMetaRegistry)
		installation.SourceRef = stringMeta(metadata, appVersionCatalogMetaSourceRef)
		installation.ProviderReleaseURL = stringMeta(metadata, appVersionCatalogMetaProviderReleaseURL)
		installation.ArtifactChecksums = stringMapMeta(metadata, appVersionCatalogMetaArtifactChecksums)
		if installedAt, ok := timeMeta(metadata, appVersionCatalogMetaInstalledAt); ok {
			installation.InstalledAt = installedAt
		}
	}
	if installation.InstalledAt.IsZero() {
		installation.InstalledAt = addedAt
	}
	return installation
}

// InstallationFromVersionAddedRecord projects one version_added record into an AppInstallation.
func InstallationFromVersionAddedRecord(record *core.AppVersionCatalogRecord) *core.AppInstallation {
	return installationFromVersionAddedRecord(record)
}

func stringMeta(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func stringMapMeta(metadata map[string]any, key string) map[string]string {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]string:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	case map[string]any:
		if len(typed) == 0 {
			return nil
		}
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = fmt.Sprint(v)
		}
		return out
	default:
		return nil
	}
}

func timeMeta(metadata map[string]any, key string) (time.Time, bool) {
	raw := stringMeta(metadata, key)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, false
		}
	}
	return parsed.UTC().Truncate(time.Millisecond), true
}
