package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/pluginpkg"
	"github.com/valon-technologies/gestalt/internal/registry"
)

// Validate loads daemon dependencies and integration factories without
// starting the server or running migrations. Unlike Bootstrap, provider
// validation is strict: any provider construction failure is returned.
func Validate(ctx context.Context, cfg *config.Config, factories *FactoryRegistry) ([]string, error) {
	sm, err := buildSecretManager(cfg, factories)
	if err != nil {
		return nil, err
	}
	defer closeSecretManager(sm)

	if err := resolveSecretRefs(ctx, cfg, sm); err != nil {
		return nil, err
	}

	deps := Deps{
		EncryptionKey: crypto.DeriveKey(cfg.Server.EncryptionKey),
		BaseURL:       cfg.Server.BaseURL,
		SecretManager: sm,
		DevMode:       cfg.Server.DevMode,
	}

	if _, err := buildAuth(cfg, factories, deps); err != nil {
		return nil, err
	}

	ds, err := buildDatastore(cfg, factories, deps)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ds.Close() }()
	deps.Egress = newEgressDeps(cfg)

	var warnings []string
	if w, ok := ds.(interface{ Warnings() []string }); ok {
		warnings = w.Warnings()
	}

	providers, err := buildProvidersStrict(ctx, cfg, factories, deps)
	if err != nil {
		return warnings, err
	}
	defer func() { _ = CloseProviders(providers) }()

	sharedInvoker := invocation.NewBroker(providers, ds)
	wireCredentialResolver(&deps.Egress, sm)
	audit := core.AuditSink(invocation.LogAuditSink{})

	extensions, err := buildExtensionsWith(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, deps.Egress, buildRuntimeForValidation)
	if err != nil {
		return warnings, err
	}
	if extensions != nil {
		// Validation does not start runtimes, but extension factories may still
		// allocate resources that need to be released after construction.
		defer func() { _ = extensions.Shutdown(context.Background()) }()
	}

	return warnings, nil
}

func buildProvidersStrict(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, deps Deps) (*registry.PluginMap[core.Provider], error) {
	reg := registry.New()

	for _, builtin := range factories.Builtins {
		if err := reg.Providers.Register(builtin.Name(), builtin); errors.Is(err, core.ErrAlreadyRegistered) {
			continue
		} else if err != nil {
			_ = CloseProviders(&reg.Providers)
			return nil, fmt.Errorf("bootstrap: registering builtin %q: %w", builtin.Name(), err)
		}
	}

	names := slices.Sorted(maps.Keys(cfg.Integrations))

	var errs []error
	for _, name := range names {
		intgDef := cfg.Integrations[name]
		prov, err := buildProviderForValidation(ctx, name, intgDef, factories, deps)
		if err != nil {
			errs = append(errs, fmt.Errorf("integration %q: %w", name, err))
			continue
		}
		if err := reg.Providers.Register(name, prov); err != nil {
			if c, ok := prov.(io.Closer); ok {
				_ = c.Close()
			}
			errs = append(errs, fmt.Errorf("bootstrap: registering provider %q: %w", name, err))
		}
	}

	if len(errs) > 0 {
		_ = CloseProviders(&reg.Providers)
		return nil, fmt.Errorf("bootstrap: provider validation failed: %w", errors.Join(errs...))
	}

	return &reg.Providers, nil
}

func buildProviderForValidation(ctx context.Context, name string, intg config.IntegrationDef, factories *FactoryRegistry, deps Deps) (core.Provider, error) {
	if intg.Plugin == nil || intg.Plugin.Package == "" || intg.Plugin.ResolvedManifestPath == "" {
		return buildProvider(ctx, name, intg, factories, deps)
	}
	return newPreparedProviderStub(name, intg, intg.Plugin.ResolvedManifestPath)
}

func buildRuntimeForValidation(ctx context.Context, name string, cfg config.RuntimeDef, factories *FactoryRegistry, deps RuntimeDeps) (core.Runtime, error) {
	if cfg.Plugin != nil && cfg.Plugin.Package != "" && cfg.Plugin.ResolvedManifestPath != "" {
		return &preparedRuntimeStub{name: name}, nil
	}
	return buildRuntime(ctx, name, cfg, factories, deps)
}

type preparedProviderStub struct {
	name           string
	displayName    string
	description    string
	connectionMode core.ConnectionMode
}

func newPreparedProviderStub(name string, intg config.IntegrationDef, manifestPath string) (core.Provider, error) {
	_, manifest, err := pluginpkg.ReadManifestFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read prepared manifest: %w", err)
	}
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
		connectionMode: connectionModeFromConnections(intg.Connections),
	}, nil
}

func (p *preparedProviderStub) Name() string                        { return p.name }
func (p *preparedProviderStub) DisplayName() string                 { return p.displayName }
func (p *preparedProviderStub) Description() string                 { return p.description }
func (p *preparedProviderStub) ConnectionMode() core.ConnectionMode { return p.connectionMode }
func (p *preparedProviderStub) ListOperations() []core.Operation    { return nil }
func (p *preparedProviderStub) Execute(context.Context, string, map[string]any, string) (*core.OperationResult, error) {
	return nil, fmt.Errorf("prepared validation stub cannot execute operations")
}

type preparedRuntimeStub struct {
	name string
}

func (r *preparedRuntimeStub) Name() string                { return r.name }
func (r *preparedRuntimeStub) Start(context.Context) error { return nil }
func (r *preparedRuntimeStub) Stop(context.Context) error  { return nil }

func connectionModeFromConnections(conns map[string]config.ConnectionDef) core.ConnectionMode {
	return core.ConnectionMode(config.ConnectionMode(conns))
}

func closeSecretManager(sm core.SecretManager) {
	if closer, ok := sm.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
