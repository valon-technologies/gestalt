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
	appVersionChangeRequestMetaRegistry           = "registry"
	appVersionChangeRequestMetaSourceRepository   = "source_repository"
	appVersionChangeRequestMetaSourceRef          = "source_ref"
	appVersionChangeRequestMetaProviderReleaseURL = "provider_release_url"
	appVersionChangeRequestMetaArtifactChecksums  = "artifact_checksums"
	appVersionChangeRequestMetaInstalledAt        = "installed_at"
)

type AppDesiredRevision struct {
	App             string
	Version         string
	ChangeRequestID string
}

func ChangeRequestMetadata(installation *core.AppInstallation) map[string]any {
	if installation == nil {
		return nil
	}
	metadata := map[string]any{
		appVersionChangeRequestMetaRegistry:           strings.TrimSpace(installation.Registry),
		appVersionChangeRequestMetaSourceRepository:   strings.TrimSpace(installation.SourceRepository),
		appVersionChangeRequestMetaSourceRef:          strings.TrimSpace(installation.SourceRef),
		appVersionChangeRequestMetaProviderReleaseURL: strings.TrimSpace(installation.ProviderReleaseURL),
	}
	if len(installation.ArtifactChecksums) > 0 {
		metadata[appVersionChangeRequestMetaArtifactChecksums] = installation.ArtifactChecksums
	}
	if !installation.InstalledAt.IsZero() {
		metadata[appVersionChangeRequestMetaInstalledAt] = installation.InstalledAt.UTC().Format(time.RFC3339Nano)
	}
	return metadata
}

func (s *AppVersionChangeRequestService) ListKnownVersionsByApp(ctx context.Context, appName string) ([]*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("list known app versions: change request service is not configured")
	}
	requests, err := s.ListRequestsByApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	return knownVersionsFromRequests(requests), nil
}

func (s *AppVersionChangeRequestService) ListAllKnownVersions(ctx context.Context) ([]*core.AppInstallation, error) {
	if s == nil {
		return nil, fmt.Errorf("list all known app versions: change request service is not configured")
	}
	requests, err := s.listAllRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all known app versions: %w", err)
	}
	return knownVersionsFromRequests(requests), nil
}

func (s *AppVersionChangeRequestService) ListDesiredRevisions(ctx context.Context) ([]AppDesiredRevision, error) {
	if s == nil {
		return nil, fmt.Errorf("list desired app revisions: change request service is not configured")
	}
	requests, err := s.listAllRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list desired app revisions: %w", err)
	}
	installationsByApp := make(map[string][]*core.AppInstallation)
	for _, installation := range knownVersionsFromRequests(requests) {
		if installation == nil {
			continue
		}
		app := strings.TrimSpace(installation.AppName)
		if app != "" {
			installationsByApp[app] = append(installationsByApp[app], installation)
		}
	}
	requestsByApp := make(map[string][]*core.AppVersionChangeRequest)
	for _, request := range requests {
		if request == nil {
			continue
		}
		app := strings.TrimSpace(request.App)
		if app != "" {
			requestsByApp[app] = append(requestsByApp[app], request)
		}
	}
	out := make([]AppDesiredRevision, 0, len(installationsByApp))
	for app, installations := range installationsByApp {
		version := LatestKnownVersion(installations)
		out = append(out, AppDesiredRevision{
			App:             app,
			Version:         version,
			ChangeRequestID: latestDesiredRevisionID(requestsByApp[app], version),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].App < out[j].App
	})
	return out, nil
}

func (s *AppVersionChangeRequestService) listAllRequests(ctx context.Context) ([]*core.AppVersionChangeRequest, error) {
	recs, err := s.store.GetAll(ctx, nil)
	if err != nil {
		return nil, err
	}
	requests := make([]*core.AppVersionChangeRequest, 0, len(recs))
	for _, rec := range recs {
		requests = append(requests, recordToAppVersionChangeRequest(rec))
	}
	return requests, nil
}

func knownVersionsFromRequests(requests []*core.AppVersionChangeRequest) []*core.AppInstallation {
	byAppVersion := make(map[string]*core.AppInstallation)
	for _, request := range requests {
		if request == nil {
			continue
		}
		appName := strings.TrimSpace(request.App)
		version := strings.TrimSpace(request.ToVersion)
		if appName == "" || version == "" {
			continue
		}
		installation := installationFromChangeRequest(request)
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

func installationFromChangeRequest(request *core.AppVersionChangeRequest) *core.AppInstallation {
	if request == nil {
		return nil
	}
	requestedAt := request.Timestamp.UTC().Truncate(time.Millisecond)
	installation := &core.AppInstallation{
		AppName:     strings.TrimSpace(request.App),
		Version:     strings.TrimSpace(request.ToVersion),
		InstalledBy: strings.TrimSpace(request.Actor),
		InstalledAt: requestedAt,
		UpdatedAt:   requestedAt,
	}
	if metadata := request.Metadata; metadata != nil {
		installation.Registry = stringMeta(metadata, appVersionChangeRequestMetaRegistry)
		installation.SourceRepository = stringMeta(metadata, appVersionChangeRequestMetaSourceRepository)
		installation.SourceRef = stringMeta(metadata, appVersionChangeRequestMetaSourceRef)
		installation.ProviderReleaseURL = stringMeta(metadata, appVersionChangeRequestMetaProviderReleaseURL)
		installation.ArtifactChecksums = stringMapMeta(metadata, appVersionChangeRequestMetaArtifactChecksums)
		if installedAt, ok := timeMeta(metadata, appVersionChangeRequestMetaInstalledAt); ok {
			installation.InstalledAt = installedAt
		}
	}
	if installation.InstalledAt.IsZero() {
		installation.InstalledAt = requestedAt
	}
	return installation
}

func InstallationFromChangeRequest(request *core.AppVersionChangeRequest) *core.AppInstallation {
	return installationFromChangeRequest(request)
}

func LatestKnownVersion(installations []*core.AppInstallation) string {
	latest := LatestKnownInstallation(installations)
	if latest == nil {
		return ""
	}
	return strings.TrimSpace(latest.Version)
}

// LatestKnownInstallation returns the local installation record that currently
// represents the desired version. Keeping the full record is important for
// projections that need immutable publication metadata, not just its version.
func LatestKnownInstallation(installations []*core.AppInstallation) *core.AppInstallation {
	if len(installations) == 0 {
		return nil
	}
	latest := installations[0]
	for _, installation := range installations[1:] {
		if installation == nil {
			continue
		}
		if latest == nil ||
			installation.UpdatedAt.After(latest.UpdatedAt) ||
			(installation.UpdatedAt.Equal(latest.UpdatedAt) &&
				strings.TrimSpace(installation.Version) > strings.TrimSpace(latest.Version)) {
			latest = installation
		}
	}
	return latest
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
