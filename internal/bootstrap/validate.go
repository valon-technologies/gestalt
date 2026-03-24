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
	audit := core.AuditSink(invocation.LogAuditSink{})

	runtimes, err := buildRuntimes(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit)
	if err != nil {
		return warnings, err
	}
	// Validation does not start runtimes, but factories may still allocate
	// resources that need to be released after construction.
	if runtimes != nil {
		defer func() { _ = StopRuntimes(context.Background(), runtimes, runtimes.List()) }()
	}

	bindings, err := buildBindings(ctx, cfg, factories, sharedInvoker, sharedInvoker, audit)
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
		factory, ok := factories.Providers[name]
		if !ok {
			factory = factories.DefaultProvider
		}
		if factory == nil {
			errs = append(errs, fmt.Errorf("bootstrap: no provider factory for %q and no default factory registered", name))
			continue
		}

		prov, err := factory(ctx, name, intgDef, deps)
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

func closeSecretManager(sm core.SecretManager) {
	if closer, ok := sm.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
