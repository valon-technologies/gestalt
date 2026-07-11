package appregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const RegistryInstallSubdir = "registry-installed"

// Installer writes shared install state and materializes registry apps on the
// handling instance.
type Installer struct {
	Registries    map[string]config.AppRegistryConfig
	Reader        *RegistryReader
	Installations *coredata.AppInstallationService
	Events        *coredata.AppInstallationEventService
	ArtifactsDir  string
	Now           func() time.Time
	installLocks  sync.Map // app name -> *sync.Mutex
}

type InstallInput struct {
	Registry string
	App      string
	Version  string
	Actor    string
}

type InstallOutput struct {
	Installation     *core.AppInstallation
	MaterializedPath string
}

func (i *Installer) Install(ctx context.Context, input InstallInput) (*InstallOutput, error) {
	if i == nil {
		return nil, fmt.Errorf("app registry installer is not configured")
	}
	registryName := strings.TrimSpace(input.Registry)
	appName := strings.TrimSpace(input.App)
	version := strings.TrimSpace(input.Version)
	actor := strings.TrimSpace(input.Actor)
	if registryName == "" {
		return nil, fmt.Errorf("registry is required")
	}
	if appName == "" {
		return nil, fmt.Errorf("app is required")
	}
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}
	if err := providerregistry.ValidateRepositoryName(appName); err != nil {
		return nil, fmt.Errorf("invalid app name: %w", err)
	}

	unlock := i.lockInstall(appName)
	defer unlock()

	if i.Installations == nil || i.Events == nil {
		return nil, fmt.Errorf("app installation services are not configured")
	}
	artifactsDir := strings.TrimSpace(i.ArtifactsDir)
	if artifactsDir == "" {
		return nil, fmt.Errorf("artifacts directory is not configured")
	}
	registry, ok := i.Registries[registryName]
	if !ok {
		return nil, fmt.Errorf("app registry not found")
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return nil, fmt.Errorf("unsupported app registry kind")
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return nil, fmt.Errorf("app registry public URL is invalid: %w", err)
	}

	reader := i.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	entry, err := reader.FetchEntry(ctx, publicRoot, appName, version)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry entry: %w", err)
	}
	if entry.App != appName {
		return nil, fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName)
	}
	if entry.Version != version {
		return nil, fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version)
	}

	platform := providerpkg.CurrentPlatformString()
	artifact, ok := entry.Artifacts[platform]
	if !ok {
		return nil, fmt.Errorf("registry entry has no artifact for platform %q", platform)
	}
	artifactURL := strings.TrimSpace(artifact.PublicURL)
	if artifactURL == "" {
		artifactURL = strings.TrimSpace(artifact.URL)
	}
	if artifactURL == "" {
		return nil, fmt.Errorf("registry entry artifact for platform %q has no download URL", platform)
	}
	expectedSHA := strings.TrimSpace(artifact.SHA256)
	if expectedSHA == "" {
		return nil, fmt.Errorf("registry entry artifact for platform %q is missing sha256", platform)
	}

	var restoreBaseline *core.AppInstallation
	previousVersion := ""
	existing, existingErr := i.Installations.GetInstallation(ctx, appName)
	if existingErr == nil {
		restoreBaseline = restoreBaselineForInstall(existing)
		previousVersion = previousVersionForInstall(existing)
	} else if !errors.Is(existingErr, core.ErrNotFound) {
		return nil, fmt.Errorf("load existing app installation: %w", existingErr)
	}

	entryURL := PublicURL(publicRoot, AppVersionEntryPath(appName, version))
	checksums := map[string]string{platform: expectedSHA}
	pending := &core.AppInstallation{
		AppName:                 appName,
		VersionConstraint:       version,
		ResolvedVersion:         version,
		SourceRef:               entry.SourceRef,
		Registry:                registryName,
		ProviderReleaseURL:      entryURL,
		ArtifactChecksums:       checksums,
		RolloutStatus:           core.AppInstallationRolloutStatusPending,
		PreviousResolvedVersion: previousVersion,
		InstalledBy:             actor,
	}

	if existingErr == core.ErrNotFound {
		if _, err := i.Installations.PutInstallation(ctx, pending); err != nil {
			return nil, fmt.Errorf("write pending app installation: %w", err)
		}
	} else {
		baseline := existing
		if _, err := i.Installations.UpdateInstallation(ctx, appName, func(installation *core.AppInstallation) error {
			applyPendingInstall(installation, pending, baseline)
			installation.AppName = appName
			return nil
		}); err != nil {
			return nil, fmt.Errorf("write pending app installation: %w", err)
		}
	}
	if _, err := i.Events.AppendEvent(ctx, &core.AppInstallationEvent{
		InstallationID: appName,
		FromVersion:    previousVersion,
		ToVersion:      version,
		Type:           core.AppInstallationEventTypeInstallRequested,
		Actor:          actor,
		Metadata: map[string]any{
			"registry": registryName,
		},
	}); err != nil {
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, "", fmt.Errorf("append install_requested event: %w", err))
	}

	materializedPath := filepath.Join(artifactsDir, RegistryInstallSubdir, appName, version)
	stagingPath := materializedPath + ".staging"

	download, err := downloadRegistryArtifact(ctx, reader.client(), artifactURL)
	if err != nil {
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, "", err)
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), expectedSHA) {
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, "", fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, expectedSHA))
	}

	if err := os.RemoveAll(stagingPath); err != nil {
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, "", fmt.Errorf("reset staging directory: %w", err))
	}
	if _, err := operator.InstallPublishedPackage(download.LocalPath, stagingPath, appName); err != nil {
		_ = os.RemoveAll(stagingPath)
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, "", fmt.Errorf("materialize app artifact: %w", err))
	}

	if err := promoteStagedArtifact(stagingPath, materializedPath); err != nil {
		_ = os.RemoveAll(stagingPath)
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, "", err)
	}

	activeSince := i.now()
	stored, err := i.Installations.UpdateInstallation(ctx, appName, func(installation *core.AppInstallation) error {
		installation.VersionConstraint = version
		installation.ResolvedVersion = version
		installation.SourceRef = entry.SourceRef
		installation.Registry = registryName
		installation.ProviderReleaseURL = entryURL
		installation.ArtifactChecksums = checksums
		installation.RolloutStatus = core.AppInstallationRolloutStatusPromoted
		installation.ActiveSince = &activeSince
		installation.PreviousResolvedVersion = previousVersion
		installation.InstalledBy = actor
		return nil
	})
	if err != nil {
		return i.failInstall(ctx, appName, restoreBaseline, previousVersion, version, actor, registryName, materializedPath, fmt.Errorf("write promoted app installation: %w", err))
	}

	i.appendInstallEventBestEffort(ctx, &core.AppInstallationEvent{
		InstallationID: appName,
		FromVersion:    previousVersion,
		ToVersion:      version,
		Type:           core.AppInstallationEventTypePromoted,
		Actor:          actor,
		Metadata: map[string]any{
			"registry":          registryName,
			"materialized_path": materializedPath,
		},
	})

	return &InstallOutput{
		Installation:     stored,
		MaterializedPath: materializedPath,
	}, nil
}

func (i *Installer) failInstall(ctx context.Context, appName string, restoreBaseline *core.AppInstallation, previousVersion, version, actor, registryName, materializedPath string, cause error) (*InstallOutput, error) {
	if materializedPath != "" {
		if err := os.RemoveAll(materializedPath); err != nil {
			return nil, fmt.Errorf("%w; also failed to remove materialized artifacts: %v", cause, err)
		}
	}
	cleanupCtx := context.WithoutCancel(ctx)
	_, updateErr := i.Installations.UpdateInstallation(cleanupCtx, appName, func(installation *core.AppInstallation) error {
		if restored := restoredInstallationAfterFailure(restoreBaseline, actor, registryName); restored != nil {
			*installation = *restored
			installation.AppName = appName
			installation.RolloutStatus = core.AppInstallationRolloutStatusFailed
			return nil
		}
		installation.RolloutStatus = core.AppInstallationRolloutStatusFailed
		installation.VersionConstraint = version
		installation.ResolvedVersion = version
		installation.Registry = registryName
		installation.PreviousResolvedVersion = previousVersion
		installation.InstalledBy = actor
		installation.ActiveSince = nil
		installation.SourceRef = ""
		installation.ProviderReleaseURL = ""
		installation.ArtifactChecksums = nil
		return nil
	})
	if updateErr != nil {
		return nil, fmt.Errorf("%w; also failed to mark installation failed: %v", cause, updateErr)
	}
	i.appendInstallEventBestEffort(cleanupCtx, &core.AppInstallationEvent{
		InstallationID: appName,
		FromVersion:    previousVersion,
		ToVersion:      version,
		Type:           core.AppInstallationEventTypeFailed,
		Actor:          actor,
		Metadata: map[string]any{
			"registry": registryName,
			"error":    cause.Error(),
		},
	})
	return nil, cause
}

func promoteStagedArtifact(stagingPath, materializedPath string) error {
	// materializedPath is always {artifactsDir}/registry-installed/{app}/{newVersion}/.
	// Prior promoted versions live under sibling directories and are never moved.
	if err := os.RemoveAll(materializedPath); err != nil {
		return fmt.Errorf("reset materialization directory: %w", err)
	}
	if err := os.Rename(stagingPath, materializedPath); err != nil {
		return fmt.Errorf("promote staged app artifact: %w", err)
	}
	return nil
}

func previousVersionForInstall(existing *core.AppInstallation) string {
	if existing == nil {
		return ""
	}
	switch strings.TrimSpace(existing.RolloutStatus) {
	case core.AppInstallationRolloutStatusPending:
		return strings.TrimSpace(existing.PreviousResolvedVersion)
	case core.AppInstallationRolloutStatusPromoted, core.AppInstallationRolloutStatusFailed:
		return strings.TrimSpace(existing.ResolvedVersion)
	default:
		return ""
	}
}

func applyPendingInstall(dst, pending, baseline *core.AppInstallation) {
	dst.RolloutStatus = pending.RolloutStatus
	dst.VersionConstraint = pending.VersionConstraint
	dst.ResolvedVersion = pending.ResolvedVersion
	dst.PreviousResolvedVersion = pending.PreviousResolvedVersion
	if strings.TrimSpace(pending.InstalledBy) != "" {
		dst.InstalledBy = pending.InstalledBy
	}
	if strings.TrimSpace(pending.Registry) != "" {
		dst.Registry = pending.Registry
	}
	if shouldPreservePriorMetadata(baseline) {
		return
	}
	dst.SourceRef = pending.SourceRef
	dst.ProviderReleaseURL = pending.ProviderReleaseURL
	dst.ArtifactChecksums = pending.ArtifactChecksums
	dst.ActiveSince = nil
}

func shouldPreservePriorMetadata(baseline *core.AppInstallation) bool {
	if baseline == nil {
		return false
	}
	switch strings.TrimSpace(baseline.RolloutStatus) {
	case core.AppInstallationRolloutStatusPromoted:
		return true
	case core.AppInstallationRolloutStatusPending:
		previous := strings.TrimSpace(baseline.PreviousResolvedVersion)
		resolved := strings.TrimSpace(baseline.ResolvedVersion)
		return previous != "" && previous != resolved
	case core.AppInstallationRolloutStatusFailed:
		return strings.TrimSpace(baseline.ResolvedVersion) != ""
	default:
		return false
	}
}

func restoreBaselineForInstall(existing *core.AppInstallation) *core.AppInstallation {
	if existing == nil {
		return nil
	}
	switch strings.TrimSpace(existing.RolloutStatus) {
	case core.AppInstallationRolloutStatusPending:
		previous := strings.TrimSpace(existing.PreviousResolvedVersion)
		resolved := strings.TrimSpace(existing.ResolvedVersion)
		if previous == "" || previous == resolved {
			return nil
		}
		restored := cloneAppInstallation(existing)
		restored.RolloutStatus = core.AppInstallationRolloutStatusPromoted
		restored.VersionConstraint = previous
		restored.ResolvedVersion = previous
		return restored
	default:
		return cloneAppInstallation(existing)
	}
}

func restoredInstallationAfterFailure(prior *core.AppInstallation, actor, registryName string) *core.AppInstallation {
	if prior == nil {
		return nil
	}
	switch strings.TrimSpace(prior.RolloutStatus) {
	case core.AppInstallationRolloutStatusPromoted:
		restored := cloneAppInstallation(prior)
		if strings.TrimSpace(actor) != "" {
			restored.InstalledBy = strings.TrimSpace(actor)
		}
		if strings.TrimSpace(registryName) != "" && strings.TrimSpace(restored.Registry) == "" {
			restored.Registry = registryName
		}
		return restored
	case core.AppInstallationRolloutStatusFailed:
		if strings.TrimSpace(prior.ResolvedVersion) == "" {
			return nil
		}
		restored := cloneAppInstallation(prior)
		if strings.TrimSpace(actor) != "" {
			restored.InstalledBy = strings.TrimSpace(actor)
		}
		if strings.TrimSpace(registryName) != "" && strings.TrimSpace(restored.Registry) == "" {
			restored.Registry = registryName
		}
		return restored
	default:
		return nil
	}
}

func cloneAppInstallation(installation *core.AppInstallation) *core.AppInstallation {
	if installation == nil {
		return nil
	}
	cloned := *installation
	if installation.ActiveSince != nil {
		activeSince := *installation.ActiveSince
		cloned.ActiveSince = &activeSince
	}
	if len(installation.ArtifactChecksums) > 0 {
		cloned.ArtifactChecksums = make(map[string]string, len(installation.ArtifactChecksums))
		for platform, digest := range installation.ArtifactChecksums {
			cloned.ArtifactChecksums[platform] = digest
		}
	}
	return &cloned
}

func (i *Installer) lockInstall(appName string) func() {
	if i == nil {
		return func() {}
	}
	value, _ := i.installLocks.LoadOrStore(appName, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (i *Installer) appendInstallEventBestEffort(ctx context.Context, event *core.AppInstallationEvent) {
	if i == nil || i.Events == nil || event == nil {
		return
	}
	_, _ = i.Events.AppendEvent(context.WithoutCancel(ctx), event)
}

func (i *Installer) now() time.Time {
	if i != nil && i.Now != nil {
		return i.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}

func downloadRegistryArtifact(ctx context.Context, client *http.Client, artifactURL string) (*providerpkg.DownloadResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build artifact download request: %w", err)
	}
	result, err := providerpkg.DownloadRequest(client, req)
	if err != nil {
		return nil, fmt.Errorf("download registry artifact: %w", err)
	}
	return result, nil
}
