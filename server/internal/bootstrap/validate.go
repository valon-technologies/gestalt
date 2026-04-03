package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

// Validate loads daemon dependencies and integration factories without
// starting the server or running migrations. Unlike Bootstrap, provider
// validation is strict: any provider construction failure is returned.
func Validate(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) ([]string, error) {
	prepared, err := prepareCore(ctx, cfg, factories, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = prepared.Close(context.Background()) }()

	var warnings []string
	if w, ok := prepared.Datastore.(interface{ Warnings() []string }); ok {
		warnings = w.Warnings()
	}

	providers, _, err := buildProvidersStrict(ctx, cfg, factories, prepared.Deps)
	if err != nil {
		return warnings, err
	}
	defer func() { _ = CloseProviders(providers) }()

	if err := validateMCPCatalogs(providers); err != nil {
		return warnings, err
	}

	return warnings, nil
}

func validateMCPCatalogs(providers *registry.PluginMap[core.Provider]) error {
	for _, name := range providers.List() {
		prov, err := providers.Get(name)
		if err != nil {
			continue
		}
		cat := prov.Catalog()
		if cat == nil {
			continue
		}
		if err := cat.ValidateMCPCompat(); err != nil {
			return fmt.Errorf("integration %q: %w", name, err)
		}
	}
	return nil
}

func buildProvidersStrict(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Provider], map[string]map[string]OAuthHandler, error) {
	reg := registry.New()
	connAuth := make(map[string]map[string]OAuthHandler)
	regStore := &lazyRegStore{deps: deps}

	for _, builtin := range factories.Builtins {
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			_ = CloseProviders(&reg.Providers)
			return nil, nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
	}

	names := slices.Sorted(maps.Keys(cfg.Integrations))

	var errs []error
	for _, name := range names {
		intgDef := cfg.Integrations[name]
		result, err := buildProviderForValidation(ctx, name, intgDef, deps, regStore)
		if err != nil {
			errs = append(errs, fmt.Errorf("integration %q: %w", name, err))
			continue
		}
		if err := reg.Providers.Register(name, result.Provider); err != nil {
			closeIfPossible(result.Provider)
			errs = append(errs, fmt.Errorf("bootstrap: registering provider %q: %w", name, err))
		}
		if len(result.ConnectionAuth) > 0 {
			connAuth[name] = result.ConnectionAuth
		}
	}

	if len(errs) > 0 {
		_ = CloseProviders(&reg.Providers)
		return nil, nil, fmt.Errorf("bootstrap: provider validation failed: %w", errors.Join(errs...))
	}

	return &reg.Providers, connAuth, nil
}

func buildProviderForValidation(ctx context.Context, name string, intg config.IntegrationDef, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	if intg.Plugin == nil || intg.Plugin.Package == "" || !intg.Plugin.HasResolvedManifest() {
		return buildProvider(ctx, name, intg, deps, regStore)
	}
	prov, err := newPreparedProviderStub(name, intg)
	if err != nil {
		return nil, err
	}
	return &ProviderBuildResult{Provider: prov}, nil
}

type preparedProviderStub struct {
	name           string
	displayName    string
	description    string
	connectionMode core.ConnectionMode
}

func newPreparedProviderStub(name string, intg config.IntegrationDef) (core.Provider, error) {
	if intg.Plugin == nil || intg.Plugin.ResolvedManifest == nil {
		return nil, fmt.Errorf("prepared manifest is not resolved")
	}
	manifest := intg.Plugin.ResolvedManifest
	displayName := manifest.DisplayName
	if displayName == "" {
		displayName = name
	}
	description := manifest.Description
	if description == "" {
		description = fmt.Sprintf("prepared plugin stub for %s", name)
	}
	return &preparedProviderStub{
		name:           name,
		displayName:    displayName,
		description:    description,
		connectionMode: connectionModeFromPlugin(intg),
	}, nil
}

func (p *preparedProviderStub) Name() string                        { return p.name }
func (p *preparedProviderStub) DisplayName() string                 { return p.displayName }
func (p *preparedProviderStub) Description() string                 { return p.description }
func (p *preparedProviderStub) ConnectionMode() core.ConnectionMode { return p.connectionMode }
func (p *preparedProviderStub) Catalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name:        p.name,
		DisplayName: p.displayName,
		Description: p.description,
	}
}
func (p *preparedProviderStub) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, fmt.Errorf("prepared validation stub cannot execute operations")
}

func connectionModeFromPlugin(intg config.IntegrationDef) core.ConnectionMode {
	if intg.Plugin == nil {
		return core.ConnectionModeUser
	}
	if intg.Plugin.ConnectionMode != "" {
		return core.ConnectionMode(intg.Plugin.ConnectionMode)
	}
	if intg.Plugin.Auth != nil && intg.Plugin.Auth.Type == "none" {
		return core.ConnectionModeNone
	}
	for _, conn := range intg.Plugin.Connections {
		if conn != nil && conn.Mode != "" {
			return core.ConnectionMode(conn.Mode)
		}
	}
	return core.ConnectionModeUser
}
