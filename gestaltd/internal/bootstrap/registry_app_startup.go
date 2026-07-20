package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

type RegistryAppMaterializer interface {
	Ensure(context.Context, *core.AppInstallation) (*core.AppMaterializationResult, error)
}

type registryAppStarter interface {
	ValidateInstallation(*core.AppInstallation) error
	StartApp(context.Context, string, string) error
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
		installation := coredata.LatestKnownInstallation(known)
		if installation == nil {
			continue
		}
		if starter == nil {
			errs = append(errs, fmt.Errorf("start registry app %q: runtime installation is not configured", appName))
			continue
		}
		if err := starter.ValidateInstallation(installation); err != nil {
			errs = append(errs, fmt.Errorf("start registry app %q@%s: %w", appName, installation.Version, err))
			continue
		}
		if materializer == nil {
			errs = append(errs, fmt.Errorf("start registry app %q: runtime installation is not configured", appName))
			continue
		}
		if _, err := materializer.Ensure(ctx, installation); err != nil {
			errs = append(errs, fmt.Errorf("start registry app %q@%s: %w", appName, installation.Version, err))
			continue
		}
		if err := starter.StartApp(ctx, appName, installation.Version); err != nil {
			errs = append(errs, fmt.Errorf("start registry app %q@%s: %w", appName, installation.Version, err))
		}
	}
	return errors.Join(errs...)
}
