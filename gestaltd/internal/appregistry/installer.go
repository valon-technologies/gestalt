package appregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
)

const RegistryInstallSubdir = "registry-installed"

// installWorkTimeoutBuffer keeps bounded install work inside the lock TTL so
// another instance cannot steal an expired lock while this attempt is still running.
const installWorkTimeoutBuffer = 30 * time.Second

// Installer validates registry versions and records known versions in
// app_version_catalog. Local materialization is handled separately by the
// background catalog controller.
type Installer struct {
	Registries map[string]config.AppRegistryConfig
	Reader     *RegistryReader
	Catalog    *coredata.AppVersionCatalogService
	Locks      *coredata.AppVersionInstallLockService
	Now        func() time.Time
}

type InstallInput struct {
	Registry string
	App      string
	Version  string
	Actor    string
}

type InstallOutput struct {
	Installation *core.AppInstallation
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

	lockTTL := coredata.DefaultAppVersionInstallLockTTL
	installCtx, cancelInstall := context.WithTimeout(ctx, installWorkTimeout(lockTTL))
	defer cancelInstall()

	if i.Catalog == nil {
		return nil, fmt.Errorf("app version catalog service is not configured")
	}
	alreadyKnown, err := i.Catalog.HasKnownVersion(installCtx, appName, version)
	if err != nil {
		return nil, fmt.Errorf("check known app version: %w", err)
	}
	if alreadyKnown {
		return nil, ErrAppVersionAlreadyInstalled
	}
	registry, ok := i.Registries[registryName]
	if !ok {
		return i.failInstall(installCtx, appName, version, actor, registryName, fmt.Errorf("app registry not found"))
	}
	if strings.TrimSpace(registry.Kind) != config.AppRegistryKindGCS {
		return i.failInstall(installCtx, appName, version, actor, registryName, fmt.Errorf("unsupported app registry kind"))
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		return i.failInstall(installCtx, appName, version, actor, registryName, fmt.Errorf("app registry public URL is invalid: %w", err))
	}

	reader := i.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	entry, err := reader.FetchEntry(installCtx, publicRoot, appName, version)
	if err != nil {
		return i.failInstall(installCtx, appName, version, actor, registryName, fmt.Errorf("fetch app registry entry: %w", err))
	}
	if entry.App != appName {
		return i.failInstall(installCtx, appName, version, actor, registryName, fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName))
	}
	if entry.Version != version {
		return i.failInstall(installCtx, appName, version, actor, registryName, fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version))
	}

	entryURL := PublicURL(publicRoot, AppVersionEntryPath(appName, version))
	checksums := artifactChecksumsFromEntry(*entry)

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

	addedRecord, err := i.Catalog.AppendRecord(installCtx, &core.AppVersionCatalogRecord{
		App:       appName,
		Version:   version,
		Type:      core.AppVersionCatalogRecordTypeVersionAdded,
		Actor:     actor,
		Timestamp: addedAt,
		Metadata:  coredata.VersionAddedMetadata(known, ""),
	})
	if err != nil {
		return nil, fmt.Errorf("append version_added record: %w", err)
	}

	return &InstallOutput{
		Installation: coredata.InstallationFromVersionAddedRecord(addedRecord),
	}, nil
}

func artifactChecksumsFromEntry(entry Entry) map[string]string {
	if len(entry.Artifacts) == 0 {
		return nil
	}
	checksums := make(map[string]string, len(entry.Artifacts))
	for platform, artifact := range entry.Artifacts {
		checksums[platform] = strings.TrimSpace(artifact.SHA256)
	}
	return checksums
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

func installWorkTimeout(lockTTL time.Duration) time.Duration {
	if lockTTL <= 0 {
		lockTTL = coredata.DefaultAppVersionInstallLockTTL
	}
	if lockTTL <= installWorkTimeoutBuffer {
		return lockTTL
	}
	return lockTTL - installWorkTimeoutBuffer
}
