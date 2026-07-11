package appregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const RegistryInstallSubdir = "registry-installed"

// ErrInstallFleetStateAdvanced indicates another install changed shared fleet
// state after this attempt started.
var ErrInstallFleetStateAdvanced = errors.New("fleet state advanced during install")

// Installer writes shared install state and materializes registry apps on the
// handling instance.
type Installer struct {
	Registries    map[string]config.AppRegistryConfig
	Reader        *RegistryReader
	Installations *coredata.AppInstallationService
	Events        *coredata.AppInstallationEventService
	ArtifactsDir  string
	Now           func() time.Time
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
	publishedVersion, err := reader.FetchPublishedVersion(ctx, publicRoot, appName, version)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry published version: %w", err)
	}
	if publishedVersion.App != appName {
		return nil, fmt.Errorf("registry published version app %q does not match requested app %q", publishedVersion.App, appName)
	}
	if publishedVersion.Version != version {
		return nil, fmt.Errorf("registry published version version %q does not match requested version %q", publishedVersion.Version, version)
	}

	platform := providerpkg.CurrentPlatformString()
	artifact, ok := publishedVersion.Artifacts[platform]
	if !ok {
		return nil, fmt.Errorf("registry published version has no artifact for platform %q", platform)
	}
	artifactURL := strings.TrimSpace(artifact.PublicURL)
	if artifactURL == "" {
		artifactURL = strings.TrimSpace(artifact.URL)
	}
	if artifactURL == "" {
		return nil, fmt.Errorf("registry published version artifact for platform %q has no download URL", platform)
	}
	expectedSHA := strings.TrimSpace(artifact.SHA256)
	if expectedSHA == "" {
		return nil, fmt.Errorf("registry published version artifact for platform %q is missing sha256", platform)
	}

	var previousVersion string
	var priorInstallation *core.AppInstallation
	existing, err := i.Installations.GetInstallation(ctx, appName)
	if err == nil {
		priorInstallation = cloneAppInstallation(existing)
		previousVersion = strings.TrimSpace(existing.ResolvedVersion)
	} else if err != core.ErrNotFound {
		return nil, fmt.Errorf("load existing app installation: %w", err)
	}

	publishedVersionPublicURL := PublicURL(publicRoot, PublishedVersionPath(appName, version))
	checksums := map[string]string{platform: expectedSHA}

	pending := &core.AppInstallation{
		AppName:                 appName,
		VersionConstraint:       version,
		ResolvedVersion:         version,
		SourceRef:               publishedVersion.SourceRef,
		Registry:                registryName,
		ProviderReleaseURL:      publishedVersionPublicURL,
		ArtifactChecksums:       checksums,
		RolloutStatus:           core.AppInstallationRolloutStatusPending,
		PreviousResolvedVersion: previousVersion,
		InstalledBy:             strings.TrimSpace(input.Actor),
	}
	if err := i.writePendingInstall(ctx, appName, priorInstallation, pending); err != nil {
		return nil, err
	}
	if _, err := i.Events.AppendEvent(ctx, &core.AppInstallationEvent{
		InstallationID: appName,
		FromVersion:    previousVersion,
		ToVersion:      version,
		Type:           core.AppInstallationEventTypeInstallRequested,
		Actor:          strings.TrimSpace(input.Actor),
		Metadata: map[string]any{
			"registry": registryName,
		},
	}); err != nil {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("append install_requested event: %w", err))
	}

	materializedPath := filepath.Join(artifactsDir, RegistryInstallSubdir, appName, version)
	stagingPath := materializedPath + ".staging"

	download, err := downloadRegistryArtifact(ctx, reader.client(), artifactURL)
	if err != nil {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, err)
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), expectedSHA) {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, expectedSHA))
	}

	if err := os.RemoveAll(stagingPath); err != nil {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("reset staging directory: %w", err))
	}
	stagingCleanup := true
	defer func() {
		if stagingCleanup {
			_ = os.RemoveAll(stagingPath)
		}
	}()

	if _, err := operator.InstallPublishedPackage(download.LocalPath, stagingPath, appName); err != nil {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("materialize app artifact: %w", err))
	}
	backupPath := materializedPath + ".backup"
	if err := os.RemoveAll(backupPath); err != nil {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("reset materialization backup directory: %w", err))
	}
	if _, err := os.Stat(materializedPath); err == nil {
		if err := os.Rename(materializedPath, backupPath); err != nil {
			return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("backup current materialization directory: %w", err))
		}
	} else if !os.IsNotExist(err) {
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("stat materialization directory: %w", err))
	}
	if err := os.Rename(stagingPath, materializedPath); err != nil {
		if _, statErr := os.Stat(backupPath); statErr == nil {
			_ = os.RemoveAll(materializedPath)
			_ = os.Rename(backupPath, materializedPath)
		}
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("promote staged app artifact: %w", err))
	}
	stagingCleanup = false
	_ = os.RemoveAll(backupPath)

	activeSince := i.now()
	stored, err := i.Installations.UpdateInstallation(ctx, appName, func(installation *core.AppInstallation) error {
		if !installationMatchesFailedAttempt(installation, version) {
			return fmt.Errorf("%w: pending install for version %q no longer active", ErrInstallFleetStateAdvanced, version)
		}
		installation.VersionConstraint = version
		installation.ResolvedVersion = version
		installation.SourceRef = publishedVersion.SourceRef
		installation.Registry = registryName
		installation.ProviderReleaseURL = publishedVersionPublicURL
		installation.ArtifactChecksums = checksums
		installation.RolloutStatus = core.AppInstallationRolloutStatusPromoted
		installation.ActiveSince = &activeSince
		installation.PreviousResolvedVersion = previousVersion
		installation.InstalledBy = strings.TrimSpace(input.Actor)
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInstallFleetStateAdvanced) {
			i.appendInstallEventBestEffort(ctx, &core.AppInstallationEvent{
				InstallationID: appName,
				FromVersion:    previousVersion,
				ToVersion:      version,
				Type:           core.AppInstallationEventTypeFailed,
				Actor:          strings.TrimSpace(input.Actor),
				Metadata: map[string]any{
					"registry": registryName,
					"error":    "fleet state advanced past pending install",
					"skipped":  "fleet state advanced past this install attempt",
				},
			})
			return nil, err
		}
		return i.failInstall(ctx, appName, priorInstallation, previousVersion, version, input.Actor, registryName, fmt.Errorf("write promoted app installation: %w", err))
	}
	i.appendInstallEventBestEffort(ctx, &core.AppInstallationEvent{
		InstallationID: appName,
		FromVersion:    previousVersion,
		ToVersion:      version,
		Type:           core.AppInstallationEventTypePromoted,
		Actor:          strings.TrimSpace(input.Actor),
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

func (i *Installer) failInstall(ctx context.Context, appName string, priorInstallation *core.AppInstallation, previousVersion, version, actor, registryName string, cause error) (*InstallOutput, error) {
	cleanupCtx := context.WithoutCancel(ctx)
	current, getErr := i.Installations.GetInstallation(cleanupCtx, appName)
	if getErr != nil && getErr != core.ErrNotFound {
		return nil, fmt.Errorf("%w; also failed to load current installation: %v", cause, getErr)
	}
	if !installationMatchesFailedAttempt(current, version) {
		i.appendInstallEventBestEffort(cleanupCtx, &core.AppInstallationEvent{
			InstallationID: appName,
			FromVersion:    previousVersion,
			ToVersion:      version,
			Type:           core.AppInstallationEventTypeFailed,
			Actor:          strings.TrimSpace(actor),
			Metadata: map[string]any{
				"registry": registryName,
				"error":    cause.Error(),
				"skipped":  "fleet state advanced past this install attempt",
			},
		})
		return nil, cause
	}

	_, putErr := i.Installations.UpdateInstallation(cleanupCtx, appName, func(installation *core.AppInstallation) error {
		if !installationMatchesFailedAttempt(installation, version) {
			return fmt.Errorf("%w: pending install for version %q no longer active during failure handling", ErrInstallFleetStateAdvanced, version)
		}
		if restored := restoredInstallationAfterFailure(priorInstallation, version, actor, registryName); restored != nil {
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
		installation.InstalledBy = strings.TrimSpace(actor)
		installation.ActiveSince = nil
		installation.SourceRef = ""
		installation.ProviderReleaseURL = ""
		installation.ArtifactChecksums = nil
		return nil
	})
	if putErr != nil {
		if errors.Is(putErr, ErrInstallFleetStateAdvanced) {
			i.appendInstallEventBestEffort(cleanupCtx, &core.AppInstallationEvent{
				InstallationID: appName,
				FromVersion:    previousVersion,
				ToVersion:      version,
				Type:           core.AppInstallationEventTypeFailed,
				Actor:          strings.TrimSpace(actor),
				Metadata: map[string]any{
					"registry": registryName,
					"error":    cause.Error(),
					"skipped":  "fleet state advanced past this install attempt",
				},
			})
			return nil, cause
		}
		return nil, fmt.Errorf("%w; also failed to mark installation failed: %v", cause, putErr)
	}
	if _, eventErr := i.Events.AppendEvent(cleanupCtx, &core.AppInstallationEvent{
		InstallationID: appName,
		FromVersion:    previousVersion,
		ToVersion:      version,
		Type:           core.AppInstallationEventTypeFailed,
		Actor:          strings.TrimSpace(actor),
		Metadata: map[string]any{
			"registry": registryName,
			"error":    cause.Error(),
		},
	}); eventErr != nil {
		return nil, fmt.Errorf("%w; also failed to append failed event: %v", cause, eventErr)
	}
	return nil, cause
}

func (i *Installer) appendInstallEventBestEffort(ctx context.Context, event *core.AppInstallationEvent) {
	if i == nil || i.Events == nil || event == nil {
		return
	}
	_, _ = i.Events.AppendEvent(context.WithoutCancel(ctx), event)
}

func restoredInstallationAfterFailure(prior *core.AppInstallation, attemptedVersion, actor, registryName string) *core.AppInstallation {
	if prior == nil {
		return nil
	}
	switch prior.RolloutStatus {
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

func (i *Installer) writePendingInstall(ctx context.Context, appName string, baseline *core.AppInstallation, pending *core.AppInstallation) error {
	if baseline == nil {
		current, err := i.Installations.GetInstallation(ctx, appName)
		if err != nil && err != core.ErrNotFound {
			return fmt.Errorf("load current app installation: %w", err)
		}
		if current != nil {
			return fmt.Errorf("%w: install baseline no longer current", ErrInstallFleetStateAdvanced)
		}
		if _, err := i.Installations.PutInstallation(ctx, pending); err != nil {
			return fmt.Errorf("write pending app installation: %w", err)
		}
		return nil
	}

	_, err := i.Installations.UpdateInstallation(ctx, appName, func(installation *core.AppInstallation) error {
		if !installationMatchesInstallBaseline(installation, baseline) {
			return fmt.Errorf("%w: install baseline no longer current", ErrInstallFleetStateAdvanced)
		}
		applyPendingInstall(installation, pending)
		return nil
	})
	if err != nil {
		return fmt.Errorf("write pending app installation: %w", err)
	}
	return nil
}

func applyPendingInstall(dst *core.AppInstallation, pending *core.AppInstallation) {
	dst.RolloutStatus = pending.RolloutStatus
	dst.VersionConstraint = pending.VersionConstraint
	dst.ResolvedVersion = pending.ResolvedVersion
	dst.SourceRef = pending.SourceRef
	dst.Registry = pending.Registry
	dst.ProviderReleaseURL = pending.ProviderReleaseURL
	dst.ArtifactChecksums = pending.ArtifactChecksums
	dst.PreviousResolvedVersion = pending.PreviousResolvedVersion
	dst.InstalledBy = pending.InstalledBy
	dst.ActiveSince = nil
}

func installationMatchesInstallBaseline(current, baseline *core.AppInstallation) bool {
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

func installationMatchesFailedAttempt(current *core.AppInstallation, attemptedVersion string) bool {
	if current == nil {
		return false
	}
	attemptedVersion = strings.TrimSpace(attemptedVersion)
	if attemptedVersion == "" {
		return false
	}
	return strings.TrimSpace(current.RolloutStatus) == core.AppInstallationRolloutStatusPending &&
		strings.TrimSpace(current.ResolvedVersion) == attemptedVersion
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
