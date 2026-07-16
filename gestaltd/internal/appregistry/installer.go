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

const (
	DefaultRolloutEnrollmentWindow = 2 * DefaultCatalogPollInterval
	DefaultRolloutTimeout          = 15 * time.Minute
)

// installWorkTimeoutBuffer keeps bounded install work inside the lock TTL so
// another instance cannot steal an expired lock while this attempt is still running.
const installWorkTimeoutBuffer = 30 * time.Second

type Installer struct {
	Registries     map[string]config.AppRegistryConfig
	ConfigApps     map[string]*config.ProviderEntry
	Reader         *RegistryReader
	ChangeRequests *coredata.AppVersionChangeRequestService
	Locks          *coredata.AppVersionInstallLockService
	Rollouts       *coredata.AppRolloutService
	Now            func() time.Time
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

	if i.ChangeRequests == nil {
		return nil, fmt.Errorf("app version change request service is not configured")
	}
	if i.Rollouts == nil {
		return nil, fmt.Errorf("app rollout service is not configured")
	}
	alreadyKnown, err := i.ChangeRequests.HasKnownVersion(installCtx, appName, version)
	if err != nil {
		return nil, fmt.Errorf("check known app version: %w", err)
	}
	if alreadyKnown {
		return nil, ErrAppVersionAlreadyInstalled
	}
	if current, getErr := i.Rollouts.Get(installCtx, appName); getErr == nil {
		if current.State == core.AppRolloutStateEnrolling || current.State == core.AppRolloutStateRestarting {
			return nil, ErrAppRolloutActive
		}
	} else if !errors.Is(getErr, core.ErrNotFound) {
		return nil, fmt.Errorf("check active app rollout: %w", getErr)
	}

	knownVersions, err := i.ChangeRequests.ListKnownVersionsByApp(installCtx, appName)
	if err != nil {
		return nil, fmt.Errorf("list known app versions: %w", err)
	}
	var configEntry *config.ProviderEntry
	if i.ConfigApps != nil {
		configEntry = i.ConfigApps[appName]
	}
	fromVersion := resolveFromVersion(knownVersions, configEntry)
	if fromVersion == "" {
		return nil, fmt.Errorf("resolve from_version: no known fleet version and app is not pinned in config")
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
	entry, err := reader.FetchEntry(installCtx, publicRoot, appName, version)
	if err != nil {
		return nil, fmt.Errorf("fetch app registry entry: %w", err)
	}
	if entry.App != appName {
		return nil, fmt.Errorf("registry entry app %q does not match requested app %q", entry.App, appName)
	}
	if entry.Version != version {
		return nil, fmt.Errorf("registry entry version %q does not match requested version %q", entry.Version, version)
	}

	entryURL := PublicURL(publicRoot, AppVersionEntryPath(appName, version))
	checksums := artifactChecksumsFromEntry(*entry)

	requestedAt := i.now()
	known := &core.AppInstallation{
		AppName:            appName,
		Version:            version,
		SourceRef:          entry.SourceRef,
		Registry:           registryName,
		ProviderReleaseURL: entryURL,
		ArtifactChecksums:  checksums,
		InstalledBy:        actor,
		InstalledAt:        requestedAt,
		UpdatedAt:          requestedAt,
	}
	_, err = i.Rollouts.Create(installCtx, &core.AppRollout{
		App:              appName,
		Version:          version,
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        requestedAt,
		EnrollmentEndsAt: requestedAt.Add(DefaultRolloutEnrollmentWindow),
		Deadline:         requestedAt.Add(DefaultRolloutTimeout),
	})
	if err != nil {
		if errors.Is(err, coredata.ErrAppRolloutActive) {
			return nil, ErrAppRolloutActive
		}
		return nil, fmt.Errorf("create app rollout: %w", err)
	}

	addedRequest, err := i.ChangeRequests.AppendRequest(installCtx, &core.AppVersionChangeRequest{
		App:         appName,
		FromVersion: fromVersion,
		ToVersion:   version,
		Actor:       actor,
		Timestamp:   requestedAt,
		Metadata:    coredata.ChangeRequestMetadata(known, ""),
	})
	if err != nil {
		_, _ = i.Rollouts.MarkFailed(context.WithoutCancel(installCtx), appName, version, i.now())
		return nil, fmt.Errorf("append change request: %w", err)
	}

	return &InstallOutput{
		Installation: coredata.InstallationFromChangeRequest(addedRequest),
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
