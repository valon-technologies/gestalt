package appregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const RegistryInstallSubdir = "registry-installed"

// Installer materializes registry apps on the handling instance and records
// known versions in app_version_catalog.
type Installer struct {
	Registries   map[string]config.AppRegistryConfig
	Reader       *RegistryReader
	Catalog      *coredata.AppVersionCatalogService
	Locks        *coredata.AppVersionInstallLockService
	ArtifactsDir string
	Now          func() time.Time
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

	if i.Locks != nil {
		lockHolder := uuid.NewString()
		if err := i.Locks.Acquire(ctx, appName, version, lockHolder, coredata.DefaultAppVersionInstallLockTTL); err != nil {
			if errors.Is(err, coredata.ErrAppVersionInstallLockHeld) {
				return nil, ErrInstallVersionLocked
			}
			return nil, fmt.Errorf("claim app version install lock: %w", err)
		}
		defer func() {
			_ = i.Locks.Release(context.WithoutCancel(ctx), appName, version, lockHolder)
		}()
	}

	if i.Catalog == nil {
		return nil, fmt.Errorf("app version catalog service is not configured")
	}
	alreadyKnown, err := i.Catalog.HasKnownVersion(ctx, appName, version)
	if err != nil {
		return nil, fmt.Errorf("check known app version: %w", err)
	}
	if alreadyKnown {
		return nil, ErrAppVersionAlreadyInstalled
	}
	artifactsDir := strings.TrimSpace(i.ArtifactsDir)
	if artifactsDir == "" {
		return nil, fmt.Errorf("artifacts directory is not configured")
	}
	registry, ok := i.Registries[registryName]
	if !ok {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("app registry not found"))
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("unsupported app registry kind"))
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("app registry public URL is invalid: %w", err))
	}

	reader := i.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	entry, err := reader.FetchEntry(ctx, publicRoot, appName, version)
	if err != nil {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("fetch app registry entry: %w", err))
	}
	if entry.App != appName {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName))
	}
	if entry.Version != version {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version))
	}

	platform := providerpkg.CurrentPlatformString()
	artifact, ok := entry.Artifacts[platform]
	if !ok {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("registry entry has no artifact for platform %q", platform))
	}
	artifactURL := strings.TrimSpace(artifact.PublicURL)
	if artifactURL == "" {
		artifactURL = strings.TrimSpace(artifact.URL)
	}
	if artifactURL == "" {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("registry entry artifact for platform %q has no download URL", platform))
	}
	expectedSHA := strings.TrimSpace(artifact.SHA256)
	if expectedSHA == "" {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("registry entry artifact for platform %q is missing sha256", platform))
	}

	entryURL := PublicURL(publicRoot, AppVersionEntryPath(appName, version))
	checksums := map[string]string{platform: expectedSHA}
	materializedPath := filepath.Join(artifactsDir, RegistryInstallSubdir, appName, version)

	download, err := downloadRegistryArtifact(ctx, reader.client(), artifactURL)
	if err != nil {
		return i.failInstall(ctx, appName, version, actor, registryName, err)
	}
	defer func() {
		if download.Cleanup != nil {
			download.Cleanup()
		}
	}()
	if !strings.EqualFold(strings.TrimSpace(download.SHA256Hex), expectedSHA) {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("artifact digest mismatch: got %s, want %s", download.SHA256Hex, expectedSHA))
	}

	if _, err := operator.InstallPublishedPackage(download.LocalPath, materializedPath, appName); err != nil {
		return i.failInstall(ctx, appName, version, actor, registryName, fmt.Errorf("materialize app artifact: %w", err))
	}

	addedAt := i.now()
	known := &core.AppInstallation{
		AppName:            appName,
		Version:            version,
		SourceRef:          entry.SourceRef,
		Registry:           registryName,
		ProviderReleaseURL: entryURL,
		ArtifactChecksums:  checksums,
		InstalledBy:        actor,
		InstalledAt:        addedAt,
		UpdatedAt:          addedAt,
	}

	addedRecord, err := i.Catalog.AppendRecord(ctx, &core.AppVersionCatalogRecord{
		App:       appName,
		Version:   version,
		Type:      core.AppVersionCatalogRecordTypeVersionAdded,
		Actor:     actor,
		Timestamp: addedAt,
		Metadata:  coredata.VersionAddedMetadata(known, materializedPath),
	})
	if err != nil {
		return nil, fmt.Errorf("append version_added record: %w", err)
	}

	return &InstallOutput{
		Installation:     coredata.InstallationFromVersionAddedRecord(addedRecord),
		MaterializedPath: materializedPath,
	}, nil
}

func (i *Installer) failInstall(ctx context.Context, appName, version, actor, registryName string, cause error) (*InstallOutput, error) {
	i.appendCatalogRecordBestEffort(context.WithoutCancel(ctx), &core.AppVersionCatalogRecord{
		App:     appName,
		Version: version,
		Type:    core.AppVersionCatalogRecordTypeInstallFailed,
		Actor:   actor,
		Metadata: map[string]any{
			"registry": registryName,
			"error":    cause.Error(),
		},
	})
	return nil, cause
}

func (i *Installer) appendCatalogRecordBestEffort(ctx context.Context, record *core.AppVersionCatalogRecord) {
	if i == nil || i.Catalog == nil || record == nil {
		return
	}
	_, _ = i.Catalog.AppendRecord(context.WithoutCancel(ctx), record)
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
