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
	Registries       map[string]config.AppRegistryConfig
	ConfigApps       map[string]*config.ProviderEntry
	Reader           *RegistryReader
	ChangeRequests   *coredata.AppVersionChangeRequestService
	Locks            *coredata.AppVersionInstallLockService
	SourceVersions   *coredata.GestaltdSourceVersionService
	Rollouts         *coredata.AppRolloutService
	RetentionCatalog RetentionCatalogStore
	GestaltdVersion  string
	SourceVersion    string
	Now              func() time.Time
}

type InstallInput struct {
	Registry string
	App      string
	Version  string
	Actor    string
}

type InstallOutput struct {
	Installation *core.AppInstallation
	FromVersion  string
	Rollout      *core.AppRollout
}

type installMode int

const (
	installModeLegacy installMode = iota
	installModeAdd
	installModeUpgrade
	installModeSelect
)

func (i *Installer) Install(ctx context.Context, input InstallInput) (*InstallOutput, error) {
	return i.install(ctx, input, installModeLegacy)
}

func (i *Installer) Add(ctx context.Context, input InstallInput) (*InstallOutput, error) {
	return i.install(ctx, input, installModeAdd)
}

func (i *Installer) Upgrade(ctx context.Context, input InstallInput) (*InstallOutput, error) {
	return i.install(ctx, input, installModeUpgrade)
}

func (i *Installer) Select(ctx context.Context, input InstallInput) (*InstallOutput, error) {
	return i.install(ctx, input, installModeSelect)
}

func (i *Installer) install(ctx context.Context, input InstallInput, mode installMode) (*InstallOutput, error) {
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
	knownVersions, err := i.ChangeRequests.ListKnownVersionsByApp(installCtx, appName)
	if err != nil {
		return nil, fmt.Errorf("list known app versions: %w", err)
	}
	var configEntry *config.ProviderEntry
	if i.ConfigApps != nil {
		configEntry = i.ConfigApps[appName]
	}
	if mode != installModeLegacy && (configEntry == nil || !configEntry.Source.IsRegistry()) {
		return nil, fmt.Errorf("%w: app %q is not registry-managed in deploy config", ErrRegistrySourceMismatch, appName)
	}
	if configEntry != nil && configEntry.Source.IsRegistry() &&
		strings.TrimSpace(configEntry.Source.Registry) != registryName {
		return nil, ErrRegistrySourceMismatch
	}
	var fromVersion string
	switch mode {
	case installModeAdd:
		if len(knownVersions) != 0 {
			return nil, ErrAppAlreadyAdded
		}
		fromVersion = "registry:first-install"
	case installModeUpgrade:
		if len(knownVersions) == 0 {
			return nil, ErrAppNotAdded
		}
		fromVersion = coredata.LatestKnownVersion(knownVersions)
	case installModeSelect:
		fromVersion = coredata.LatestKnownVersion(knownVersions)
		if fromVersion == "" {
			fromVersion = "registry:first-install"
		}
	default:
		fromVersion = resolveFromVersion(knownVersions, configEntry)
		if fromVersion == "" {
			return nil, fmt.Errorf("resolve from_version: no known fleet version and app is not pinned in config")
		}
	}
	if mode != installModeSelect {
		alreadyKnown, err := i.ChangeRequests.HasKnownVersion(installCtx, appName, version)
		if err != nil {
			return nil, fmt.Errorf("check known app version: %w", err)
		}
		if alreadyKnown {
			return nil, ErrAppVersionAlreadyInstalled
		}
	}
	if current, getErr := i.Rollouts.Get(installCtx, appName); getErr == nil {
		if current.State == core.AppRolloutStateEnrolling || current.State == core.AppRolloutStateRestarting {
			return nil, ErrAppRolloutActive
		}
	} else if !errors.Is(getErr, core.ErrNotFound) {
		return nil, fmt.Errorf("check active app rollout: %w", getErr)
	}
	if mode == installModeSelect && coredata.LatestKnownVersion(knownVersions) == version {
		return nil, ErrAppVersionAlreadyInstalled
	}
	if strings.TrimSpace(i.SourceVersion) != "" {
		if i.SourceVersions == nil {
			return nil, fmt.Errorf("gestaltd source version service is not configured")
		}
		if _, err = i.SourceVersions.CurrentForAdmission(installCtx); err != nil {
			return nil, fmt.Errorf("resolve app rollout target source version: %w", err)
		}
	}

	reader := i.Reader
	if reader == nil {
		reader = &RegistryReader{}
	}
	registryConfig, ok := i.Registries[registryName]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAppRegistryNotConfigured, registryName)
	}
	policy, err := retentionPolicyFromConfig(registryConfig)
	if err != nil {
		return nil, err
	}
	publicRoot, err := registryConfig.PublicURL()
	if err != nil {
		return nil, fmt.Errorf("resolve registry public URL: %w", err)
	}
	retentionIndex, err := reader.FetchRetentionIndex(installCtx, publicRoot, appName)
	if err != nil {
		return nil, fmt.Errorf("fetch retention index: %w", err)
	}
	currentDesired := coredata.LatestKnownVersion(knownVersions)
	if mode == installModeSelect || mode == installModeUpgrade || mode == installModeAdd {
		if err := VersionSelectable(version, currentDesired, retentionIndex, policy, i.now()); err != nil {
			return nil, err
		}
	}

	source, err := fetchConfiguredRegistryEntry(installCtx, i.Registries, reader, registryName, appName, version)
	if err != nil {
		return nil, err
	}
	entry := source.Entry

	if err := i.validateInstallCandidate(installCtx, registryName, appName, version, entry); err != nil {
		return nil, err
	}

	entryURL := PublicURL(source.PublicRoot, AppVersionEntryPath(appName, version))
	checksums := artifactChecksumsFromEntry(*entry)

	requestedAt := i.now()
	var fromVersionDeployableUntil *time.Time
	if fromVersion != "" && fromVersion != FirstInstallFromVersion {
		deadline := requestedAt.Add(policy.DeployedRetention)
		fromVersionDeployableUntil = &deadline
	}
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
	pendingRollout := &core.AppRollout{
		App:              appName,
		Version:          version,
		State:            core.AppRolloutStateEnrolling,
		CreatedAt:        requestedAt,
		EnrollmentEndsAt: requestedAt.Add(DefaultRolloutEnrollmentWindow),
		Deadline:         requestedAt.Add(DefaultRolloutTimeout),
	}
	var rollout *core.AppRollout
	if strings.TrimSpace(i.SourceVersion) != "" {
		rollout, err = i.SourceVersions.CreateAppRollout(installCtx, pendingRollout)
	} else {
		rollout, err = i.Rollouts.Create(installCtx, pendingRollout)
	}
	if err != nil {
		if errors.Is(err, coredata.ErrAppRolloutActive) {
			return nil, ErrAppRolloutActive
		}
		return nil, fmt.Errorf("create app rollout: %w", err)
	}

	addedRequest, err := i.ChangeRequests.AppendRequest(installCtx, &core.AppVersionChangeRequest{
		App:                        appName,
		FromVersion:                fromVersion,
		ToVersion:                  version,
		Actor:                      actor,
		Timestamp:                  requestedAt,
		FromVersionDeployableUntil: fromVersionDeployableUntil,
		Metadata:                   coredata.ChangeRequestMetadata(known),
	})
	if err != nil {
		_, _ = i.Rollouts.MarkFailed(context.WithoutCancel(installCtx), appName, version, i.now())
		return nil, fmt.Errorf("append change request: %w", err)
	}
	i.mirrorRetentionTransition(installCtx, registryName, appName, fromVersion, version, policy, requestedAt)

	return &InstallOutput{
		Installation: coredata.InstallationFromChangeRequest(addedRequest),
		FromVersion:  fromVersion,
		Rollout:      rollout,
	}, nil
}

func (i *Installer) validateInstallCandidate(ctx context.Context, registryName, appName, version string, entry *Entry) error {
	validator := &InstallValidator{
		Registries:      i.Registries,
		ConfigApps:      i.ConfigApps,
		Reader:          i.Reader,
		ChangeRequests:  i.ChangeRequests,
		GestaltdVersion: i.GestaltdVersion,
	}
	return validator.Validate(ctx, ValidateInput{
		Registry: registryName,
		App:      appName,
		Version:  version,
		Entry:    entry,
	})
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

func retentionPolicyFromConfig(registry config.AppRegistryConfig) (RetentionPolicy, error) {
	unused, deployed, err := registry.RetentionPolicy()
	if err != nil {
		return RetentionPolicy{}, err
	}
	return RetentionPolicy{
		UnusedRetention:   unused,
		DeployedRetention: deployed,
	}, nil
}

func (i *Installer) mirrorRetentionTransition(ctx context.Context, registryName, appName, fromVersion, toVersion string, policy RetentionPolicy, now time.Time) {
	if i == nil || i.RetentionCatalog == nil {
		return
	}
	mirrorCtx := context.WithoutCancel(ctx)
	_ = i.RetentionCatalog.MutateRetention(mirrorCtx, registryName, appName, func(index *RetentionIndex) (bool, error) {
		if index == nil {
			return false, nil
		}
		return ApplyDesiredVersionTransition(index, fromVersion, toVersion, policy, now), nil
	})
}
