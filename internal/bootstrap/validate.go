package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/crypto"
	"github.com/valon-technologies/gestalt/internal/composite"
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
	}

	if _, err := buildAuth(cfg, factories, deps); err != nil {
		return nil, err
	}

	ds, err := buildDatastore(cfg, factories, deps)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ds.Close() }()
	deps.Egress = newEgressDeps(cfg, ds)

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
	wireCredentialResolver(&deps.Egress, sharedInvoker, providers)
	audit := core.AuditSink(invocation.LogAuditSink{})

	runtimes, err := buildRuntimesForValidation(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, deps.Egress)
	if err != nil {
		return warnings, err
	}
	// Validation does not start runtimes, but factories may still allocate
	// resources that need to be released after construction.
	if runtimes != nil {
		defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()
	}

	bindings, err := buildBindings(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit, deps.Egress)
	if err != nil {
		return warnings, err
	}
	if bindings != nil {
		defer func() { _ = CloseBindings(bindings, bindings.List()) }()
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
	if intg.Plugin == nil || intg.Plugin.Ref == "" || intg.Plugin.PreparedManifestPath == "" {
		return buildProvider(ctx, name, intg, factories, deps)
	}

	mode := intg.Plugin.Mode
	if mode == "" {
		mode = config.PluginModeReplace
	}

	overlayProv, err := newPreparedProviderStub(name, intg, intg.Plugin.PreparedManifestPath)
	if err != nil {
		return nil, err
	}
	if mode != config.PluginModeOverlay {
		return overlayProv, nil
	}

	baseIntg := intg
	baseIntg.Plugin = nil
	factory, ok := factories.Providers[name]
	if !ok {
		factory = factories.DefaultProvider
	}
	if factory == nil {
		return nil, fmt.Errorf("no provider factory for overlay base %q", name)
	}
	baseProv, err := factory(ctx, name, baseIntg, deps)
	if err != nil {
		return nil, fmt.Errorf("building overlay base: %w", err)
	}
	return composite.NewOverlay(name, baseProv, overlayProv), nil
}

func buildRuntimesForValidation(ctx context.Context, cfg *config.Config, factories *FactoryRegistry, invoker invocation.Invoker, lister invocation.CapabilityLister, audit core.AuditSink, egressDeps EgressDeps) (*registry.PluginMap[core.Runtime], error) {
	if len(cfg.Runtimes) == 0 {
		return nil, nil
	}

	runtimes := registry.NewRuntimeMap()

	for name := range cfg.Runtimes {
		def := cfg.Runtimes[name]
		deps := runtimeDepsForProviders(name, invoker, lister, def.Providers, audit, egressDeps)
		rt, err := buildRuntimeForValidation(ctx, name, def, factories, deps)
		if err != nil {
			_ = StopRuntimes(context.Background(), runtimes, runtimes.List())
			return nil, fmt.Errorf("bootstrap: runtime %q: %w", name, err)
		}

		if err := runtimes.Register(name, rt); err != nil {
			_ = rt.Stop(context.Background())
			_ = StopRuntimes(context.Background(), runtimes, runtimes.List())
			return nil, fmt.Errorf("bootstrap: registering runtime %q: %w", name, err)
		}
		log.Printf("loaded runtime %s (type=%s, providers=%v)", name, def.Type, def.Providers)
	}

	return runtimes, nil
}

func buildRuntimeForValidation(ctx context.Context, name string, cfg config.RuntimeDef, factories *FactoryRegistry, deps RuntimeDeps) (core.Runtime, error) {
	if cfg.Plugin != nil && cfg.Plugin.Ref != "" && cfg.Plugin.PreparedManifestPath != "" {
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
		connectionMode: connectionModeForValidation(intg.ConnectionMode),
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

func connectionModeForValidation(raw string) core.ConnectionMode {
	switch raw {
	case string(core.ConnectionModeUser):
		return core.ConnectionModeUser
	case string(core.ConnectionModeIdentity):
		return core.ConnectionModeIdentity
	case string(core.ConnectionModeEither):
		return core.ConnectionModeEither
	case string(core.ConnectionModeNone), "":
		return core.ConnectionModeNone
	default:
		return core.ConnectionModeNone
	}
}

func closeSecretManager(sm core.SecretManager) {
	if closer, ok := sm.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
