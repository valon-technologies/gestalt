package appregistry

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

// InstallValidator checks registry install candidates before fleet admission.
type InstallValidator struct {
	Registries      map[string]config.AppRegistryConfig
	ConfigApps      map[string]*config.ProviderEntry
	Reader          *RegistryReader
	ChangeRequests  *coredata.AppVersionChangeRequestService
	GestaltdVersion string
	Platform        string
}

// ValidateInput is the candidate version to validate.
type ValidateInput struct {
	Registry string
	App      string
	Version  string
	Entry    *Entry
}

// Validate runs install-time checks against registry metadata and fleet catalog state.
func (v *InstallValidator) Validate(ctx context.Context, input ValidateInput) error {
	if v == nil {
		return fmt.Errorf("install validator is not configured")
	}
	entry := input.Entry
	if entry == nil {
		return installValidationError(InstallValidationPlatformArtifactMissing, "registry entry is required")
	}
	platform := strings.TrimSpace(v.Platform)
	if platform == "" {
		platform = providerpkg.CurrentPlatformString()
	}
	if _, err := resolveRegistryArtifact(entry, platform); err != nil {
		return installValidationError(InstallValidationPlatformArtifactMissing, err.Error())
	}
	if err := validateGestaltdCompatibility(v.gestaltdVersion(), entry.Compatibility); err != nil {
		return err
	}
	if v.ChangeRequests == nil {
		return fmt.Errorf("install validator change request service is not configured")
	}
	knownInstallations, err := v.ChangeRequests.ListAllKnownVersions(ctx)
	if err != nil {
		return fmt.Errorf("list fleet-known app versions: %w", err)
	}
	knownByApp := indexKnownInstallations(knownInstallations)
	if err := validateDeclaredDependencies(ctx, v, input, knownByApp); err != nil {
		return err
	}
	if err := validateReverseDependents(ctx, v, input, knownByApp); err != nil {
		return err
	}
	return nil
}

func (v *InstallValidator) gestaltdVersion() string {
	if v != nil && strings.TrimSpace(v.GestaltdVersion) != "" {
		return strings.TrimSpace(v.GestaltdVersion)
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		version := strings.TrimSpace(info.Main.Version)
		if version != "" && version != "(devel)" {
			return version
		}
	}
	return "dev"
}

func validateGestaltdCompatibility(running string, compatibility Compatibility) error {
	minVersion := strings.TrimSpace(compatibility.MinGestaltdVersion)
	if minVersion == "" {
		return nil
	}
	running = strings.TrimSpace(running)
	if gestaltdVersionCheckSkipped(running) {
		return nil
	}
	runningVersion, err := semver.NewVersion(running)
	if err != nil {
		return installValidationError(InstallValidationGestaltdVersionUnknown, "cannot determine running gestaltd version")
	}
	required, err := semver.NewVersion(minVersion)
	if err != nil {
		return installValidationError(
			InstallValidationGestaltdVersionInvalid,
			fmt.Sprintf("registry version has invalid minGestaltdVersion %q", minVersion),
		)
	}
	if runningVersion.LessThan(required) {
		return installValidationError(
			InstallValidationGestaltdVersionIncompatible,
			fmt.Sprintf("registry version requires gestaltd %s or newer", minVersion),
		)
	}
	return nil
}

func gestaltdVersionCheckSkipped(running string) bool {
	switch strings.TrimSpace(running) {
	case "dev", "(devel)":
		return true
	}
	// CI images stamp 0.0.0-ci+g<sha> via resolve-gestaltd-ref.yml.
	return strings.HasPrefix(strings.TrimSpace(running), "0.0.0-ci")
}

func indexKnownInstallations(installations []*core.AppInstallation) map[string]*core.AppInstallation {
	byApp := make(map[string][]*core.AppInstallation, len(installations))
	for _, installation := range installations {
		if installation == nil {
			continue
		}
		app := strings.TrimSpace(installation.AppName)
		if app == "" {
			continue
		}
		byApp[app] = append(byApp[app], installation)
	}
	latest := make(map[string]*core.AppInstallation, len(byApp))
	for app, versions := range byApp {
		version := coredata.LatestKnownVersion(versions)
		if version == "" {
			continue
		}
		for _, installation := range versions {
			if installation != nil && strings.TrimSpace(installation.Version) == version {
				latest[app] = installation
				break
			}
		}
	}
	return latest
}

func validateDeclaredDependencies(
	ctx context.Context,
	v *InstallValidator,
	input ValidateInput,
	knownByApp map[string]*core.AppInstallation,
) error {
	if len(input.Entry.Requires.Apps) == 0 {
		return nil
	}
	for depKey, requirement := range input.Entry.Requires.Apps {
		depApp, err := RequirementAppName(depKey)
		if err != nil {
			return installValidationError(
				InstallValidationDependencyNotInstalled,
				fmt.Sprintf("dependency %s: invalid app name", depKey),
			)
		}
		if configPinnedDependency(v.ConfigApps, depApp) {
			continue
		}
		installation := knownByApp[depApp]
		if installation == nil {
			return installValidationError(
				InstallValidationDependencyNotInstalled,
				fmt.Sprintf("dependency %s: fleet-known version not installed", depApp),
			)
		}
		depVersion := strings.TrimSpace(installation.Version)
		constraint := strings.TrimSpace(requirement.Version)
		if constraint != "" && !providerregistry.VersionSatisfiesFleetConstraint(depVersion, constraint) {
			return installValidationError(
				InstallValidationDependencyVersionUnsatisfied,
				fmt.Sprintf("dependency %s: fleet-known version %s does not satisfy %s", depApp, depVersion, constraint),
			)
		}
		if len(requirement.Operations) == 0 {
			continue
		}
		depEntry, err := v.fetchPublishedEntry(ctx, installation.Registry, depApp, depVersion)
		if err != nil {
			return validationFetchError(InstallValidationDependencyMetadataMissing, fmt.Sprintf("dependency %s", depApp), err)
		}
		if err := validateRequiredOperations(
			requirement.Operations,
			depEntry.Interface,
			fmt.Sprintf("dependency %s", depApp),
			InstallValidationDependencyOperationMissing,
			InstallValidationDependencyOperationSchema,
		); err != nil {
			return err
		}
	}
	return nil
}

func configPinnedDependency(configApps map[string]*config.ProviderEntry, appName string) bool {
	if configApps == nil {
		return false
	}
	entry := configApps[appName]
	if entry == nil {
		return false
	}
	if entry.Source.IsRegistry() {
		return false
	}
	return entry.Source.ResolvedPackageVersion() != "" ||
		entry.Source.IsGit() ||
		entry.Source.IsGitHubRelease() ||
		entry.Source.IsPackage() ||
		entry.Source.IsLocal() ||
		entry.Source.IsBuiltin() ||
		entry.Source.IsMetadataURL() ||
		entry.Source.IsLocalMetadataPath()
}

func validateRequiredOperations(
	required map[string]OperationRequirement,
	published Interface,
	subject string,
	missingReason InstallValidationReason,
	schemaReason InstallValidationReason,
) error {
	if len(required) == 0 {
		return nil
	}
	for opName, requirement := range required {
		opName = strings.TrimSpace(opName)
		if opName == "" {
			continue
		}
		contract, ok := published.Operations[opName]
		if !ok {
			return installValidationError(missingReason, fmt.Sprintf("%s: operation %s is not published", subject, opName))
		}
		expectedHash := strings.TrimSpace(requirement.InputSchemaHash)
		if expectedHash == "" {
			continue
		}
		actualHash := InputSchemaHash(contract.InputSchema)
		if actualHash != expectedHash {
			return installValidationError(schemaReason, fmt.Sprintf("%s: operation %s input schema hash mismatch", subject, opName))
		}
	}
	return nil
}

func validateReverseDependents(
	ctx context.Context,
	v *InstallValidator,
	input ValidateInput,
	knownByApp map[string]*core.AppInstallation,
) error {
	targetApp := strings.TrimSpace(input.App)
	for appName, installation := range knownByApp {
		if installation == nil || appName == targetApp {
			continue
		}
		dependentEntry, err := v.fetchPublishedEntry(ctx, installation.Registry, appName, installation.Version)
		if err != nil {
			if errors.Is(err, ErrRegistryDocumentNotFound) {
				continue
			}
			if errors.Is(err, ErrAppRegistryNotConfigured) {
				return fmt.Errorf("reverse dependent %s: %w", appName, err)
			}
			return validationFetchError(InstallValidationReverseDependentMetadataMissing, fmt.Sprintf("reverse dependent %s", appName), err)
		}
		requirement, ok := requirementForApp(dependentEntry.Requires, targetApp)
		if !ok {
			continue
		}
		candidateVersion := strings.TrimSpace(input.Version)
		constraint := strings.TrimSpace(requirement.Version)
		if constraint != "" && !providerregistry.VersionSatisfiesFleetConstraint(candidateVersion, constraint) {
			return installValidationError(
				InstallValidationReverseDependentVersionUnsatisfied,
				fmt.Sprintf("reverse dependent %s: candidate version %s does not satisfy %s", appName, candidateVersion, constraint),
			)
		}
		subject := fmt.Sprintf("reverse dependent %s requires candidate %s", appName, targetApp)
		if err := validateRequiredOperations(
			requirement.Operations,
			input.Entry.Interface,
			subject,
			InstallValidationReverseDependentOperationMissing,
			InstallValidationReverseDependentOperationSchema,
		); err != nil {
			return err
		}
	}
	return nil
}

func (v *InstallValidator) fetchPublishedEntry(ctx context.Context, registryName, appName, version string) (*Entry, error) {
	registryName = strings.TrimSpace(registryName)
	if registryName == "" {
		return nil, fmt.Errorf("registry is required")
	}
	source, err := fetchConfiguredRegistryEntry(ctx, v.Registries, v.reader(), registryName, appName, version)
	if err != nil {
		return nil, err
	}
	return source.Entry, nil
}

func (v *InstallValidator) reader() *RegistryReader {
	if v != nil && v.Reader != nil {
		return v.Reader
	}
	return &RegistryReader{}
}
