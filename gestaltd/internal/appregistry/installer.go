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

// Installer materializes registry apps on the handling instance and records
// install lifecycle in app_installation_events.
type Installer struct {
	Registries   map[string]config.AppRegistryConfig
	Reader       *RegistryReader
	Events       *coredata.AppInstallationEventService
	ArtifactsDir string
	Now          func() time.Time
	installLocks sync.Map // app name -> *sync.Mutex
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

	if i.Events == nil {
		return nil, fmt.Errorf("app installation event service is not configured")
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

	previousVersion := ""
	installedAt := i.now()
	supersedesEventID := ""
	head, headErr := i.Events.HeadInstallation(ctx, appName)
	if headErr == nil {
		previousVersion = strings.TrimSpace(head.ResolvedVersion)
		if !head.InstalledAt.IsZero() {
			installedAt = head.InstalledAt
		}
	} else if !errors.Is(headErr, core.ErrNotFound) {
		return nil, fmt.Errorf("load existing app installation: %w", headErr)
	}
	if headEvent, headEventErr := i.Events.HeadEvent(ctx, appName); headEventErr == nil {
		supersedesEventID = strings.TrimSpace(headEvent.ID)
	} else if !errors.Is(headEventErr, core.ErrNotFound) {
		return nil, fmt.Errorf("load existing head event: %w", headEventErr)
	}

	entryURL := PublicURL(publicRoot, AppVersionEntryPath(appName, version))
	checksums := map[string]string{platform: expectedSHA}

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
		return i.failInstall(ctx, appName, previousVersion, version, actor, registryName, "", fmt.Errorf("append install_requested event: %w", err))
	}

	materializedPath := filepath.Join(artifactsDir, RegistryInstallSubdir, appName, version)

	download, err := downloadRegistryArtifact(ctx, reader.client(), artifactURL)
	if err != nil {
		return i.failInstall(ctx, appName, previousVersion, version, actor, registryName, "", err)
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), expectedSHA) {
		return i.failInstall(ctx, appName, previousVersion, version, actor, registryName, "", fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, expectedSHA))
	}

	if _, err := operator.InstallPublishedPackage(download.LocalPath, materializedPath, appName); err != nil {
		return i.failInstall(ctx, appName, previousVersion, version, actor, registryName, materializedPath, fmt.Errorf("materialize app artifact: %w", err))
	}

	activeSince := i.now()
	promoted := &core.AppInstallation{
		AppName:                 appName,
		VersionConstraint:       version,
		ResolvedVersion:         version,
		SourceRef:               entry.SourceRef,
		Registry:                registryName,
		ProviderReleaseURL:      entryURL,
		ArtifactChecksums:       checksums,
		RolloutStatus:           core.AppInstallationRolloutStatusPromoted,
		ActiveSince:             &activeSince,
		PreviousResolvedVersion: previousVersion,
		InstalledBy:             actor,
		InstalledAt:             installedAt,
		UpdatedAt:               activeSince,
	}

	promotedEvent, err := i.Events.AppendEvent(ctx, &core.AppInstallationEvent{
		InstallationID:    appName,
		FromVersion:       previousVersion,
		ToVersion:         version,
		Type:              core.AppInstallationEventTypePromoted,
		Actor:             actor,
		Timestamp:         activeSince,
		SupersedesEventID: supersedesEventID,
		Metadata:          coredata.PromotedInstallationMetadata(promoted, materializedPath),
	})
	if err != nil {
		return i.failInstall(ctx, appName, previousVersion, version, actor, registryName, materializedPath, fmt.Errorf("append promoted event: %w", err))
	}

	return &InstallOutput{
		Installation:     coredata.InstallationFromPromotedEvent(promotedEvent),
		MaterializedPath: materializedPath,
	}, nil
}

func (i *Installer) failInstall(ctx context.Context, appName, previousVersion, version, actor, registryName, materializedPath string, cause error) (*InstallOutput, error) {
	if materializedPath != "" {
		if err := os.RemoveAll(materializedPath); err != nil {
			return nil, fmt.Errorf("%w; also failed to remove materialized artifacts: %v", cause, err)
		}
	}
	i.appendInstallEventBestEffort(context.WithoutCancel(ctx), &core.AppInstallationEvent{
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
