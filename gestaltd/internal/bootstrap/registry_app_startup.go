package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type RegistryAppMaterializer interface {
	MaterializeApp(context.Context, *core.AppInstallation) error
}

type registryAppStarter interface {
	StartApp(context.Context, string, string) error
}

type registryAppDeactivator interface {
	DeactivateApp(context.Context, string) error
}

func startRegistryOnlyAppProviders(
	ctx context.Context,
	cfg *config.Config,
	changeRequests *coredata.AppVersionChangeRequestService,
	materializer RegistryAppMaterializer,
	starter registryAppStarter,
) error {
	if cfg == nil || changeRequests == nil {
		return nil
	}
	var errs []error
	for _, appName := range slices.Sorted(maps.Keys(cfg.Apps)) {
		entry := cfg.Apps[appName]
		if entry == nil || !entry.Source.IsRegistry() {
			continue
		}
		known, err := changeRequests.ListKnownVersionsByApp(ctx, appName)
		if err != nil {
			errs = append(errs, fmt.Errorf("start registry app %q: list known versions: %w", appName, err))
			continue
		}
		version := coredata.LatestKnownVersion(known)
		if version == "" {
			if deactivator, ok := starter.(registryAppDeactivator); ok {
				if err := deactivator.DeactivateApp(ctx, appName); err != nil {
					errs = append(errs, fmt.Errorf("start registry app %q: clear stale activation: %w", appName, err))
				}
			}
			continue
		}
		installation := coredata.KnownInstallationByVersion(known, version)
		if installation == nil {
			errs = append(errs, fmt.Errorf("start registry app %q: latest known version %q is unavailable", appName, version))
			continue
		}
		if configuredRegistry := strings.TrimSpace(entry.Source.Registry); strings.TrimSpace(installation.Registry) != configuredRegistry {
			if deactivator, ok := starter.(registryAppDeactivator); ok {
				if err := deactivator.DeactivateApp(ctx, appName); err != nil {
					errs = append(errs, fmt.Errorf("start registry app %q: clear mismatched activation: %w", appName, err))
				}
			}
			errs = append(errs, fmt.Errorf("start registry app %q: known version %q belongs to registry %q, want %q", appName, version, installation.Registry, configuredRegistry))
			continue
		}
		if materializer == nil {
			errs = append(errs, fmt.Errorf("start registry app %q: app registry materializer is not configured", appName))
			continue
		}
		if err := materializer.MaterializeApp(ctx, installation); err != nil {
			errs = append(errs, fmt.Errorf("start registry app %q@%s: %w", appName, version, err))
			continue
		}
		if starter == nil {
			errs = append(errs, fmt.Errorf("start registry app %q: app provider restarter is not configured", appName))
			continue
		}
		if err := starter.StartApp(ctx, appName, version); err != nil {
			errs = append(errs, fmt.Errorf("start registry app %q@%s: %w", appName, version, err))
		}
	}
	return errors.Join(errs...)
}
