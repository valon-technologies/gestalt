package bootstrap

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/providerdrivers/componentprovider"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimeprovider"
	"gopkg.in/yaml.v3"
)

type hostedProviderLaunch struct {
	name            string
	runtimeConfig   config.EffectiveRuntimePlacement
	runtimeProvider runtimeprovider.Provider
	runtimeOwned    bool
	runtimePlan     RuntimePlacementPlan
	cfg             componentprovider.YAMLConfig
	allowedHosts    []string
	launch          hostedProcessLaunch
	cleanup         func()
}

func (p *hostedProviderLaunch) close() {
	if p == nil {
		return
	}
	if p.runtimeOwned && p.runtimeProvider != nil {
		_ = p.runtimeProvider.Close()
	}
	if p.cleanup != nil {
		p.cleanup()
		p.cleanup = nil
	}
}

func prepareHostedProviderLaunch(ctx context.Context, kind, name string, entry *config.ProviderEntry, node yaml.Node, deps Deps) (*hostedProviderLaunch, error) {
	runtimeConfig, runtimeProvider, runtimeOwned, err := effectiveConfiguredHostedRuntime(ctx, "providers."+kind+"."+name, entry, deps)
	if err != nil {
		return nil, err
	}
	if runtimeProvider == nil {
		return nil, fmt.Errorf("%s provider: runtime is required", kind)
	}
	runtimeSupport, err := runtimeProvider.Support(ctx)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, fmt.Errorf("query %s support: %w", hostedRuntimeLabel(runtimeConfig), err)
	}
	runtimePlan := buildRuntimePlacementPlan(runtimeSupport, deps, runtimeRequiresHostnameEgress(entry, deps))
	if err := runtimePlan.Validate(hostedRuntimeLabel(runtimeConfig), deps); err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}

	cfg, err := componentprovider.DecodeYAMLConfig(node, kind+" provider")
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup := func() {}
	if !hostedRuntimeUsesImageEntrypoint(runtimeConfig) {
		prepared, err := componentprovider.PrepareExecution(componentprovider.PrepareParams{
			Kind:                 kind,
			Subject:              kind + " provider",
			SourceMissingMessage: fmt.Sprintf("no Go, Rust, Python, or TypeScript %s provider source package found", kind),
			Config:               cfg,
		})
		if err != nil {
			if runtimeOwned {
				_ = runtimeProvider.Close()
			}
			return nil, err
		}
		cfg = prepared.YAMLConfig
		cleanup = prepared.Cleanup
	}
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	launch, err := prepareHostedProcessLaunch(kind, name, entry, cfg.Command, cfg.Args, cleanup, runtimeConfig)
	if err != nil {
		if runtimeOwned {
			_ = runtimeProvider.Close()
		}
		return nil, err
	}
	cleanup = launch.cleanup

	preparedLaunch := &hostedProviderLaunch{
		name:            name,
		runtimeConfig:   runtimeConfig,
		runtimeProvider: runtimeProvider,
		runtimeOwned:    runtimeOwned,
		runtimePlan:     runtimePlan,
		cfg:             cfg,
		allowedHosts:    entry.EffectiveAllowedHosts(),
		launch:          launch,
		cleanup:         cleanup,
	}
	cleanup = nil
	return preparedLaunch, nil
}
