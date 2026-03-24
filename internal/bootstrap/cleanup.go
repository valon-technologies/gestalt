package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/registry"
)

// CloseProviders closes all registered providers that implement io.Closer.
func CloseProviders(providers *registry.PluginMap[core.Provider]) error {
	if providers == nil {
		return nil
	}

	var errs []error
	for _, name := range providers.List() {
		prov, err := providers.Get(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("looking up provider %q during shutdown: %w", name, err))
			continue
		}
		c, ok := prov.(io.Closer)
		if !ok {
			continue
		}
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing provider %q: %w", name, err))
		}
	}

	return errors.Join(errs...)
}

func CloseBindings(bindings *registry.PluginMap[core.Binding], names []string) error {
	if bindings == nil {
		return nil
	}

	var errs []error
	for _, name := range names {
		binding, err := bindings.Get(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("looking up binding %q during shutdown: %w", name, err))
			continue
		}
		if err := binding.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing binding %q: %w", name, err))
		}
	}

	return errors.Join(errs...)
}

func StopRuntimes(ctx context.Context, runtimes *registry.PluginMap[core.Runtime], names []string) error {
	if runtimes == nil {
		return nil
	}

	var errs []error
	for _, name := range names {
		rt, err := runtimes.Get(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("looking up runtime %q during shutdown: %w", name, err))
			continue
		}
		if err := rt.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stopping runtime %q: %w", name, err))
		}
	}

	return errors.Join(errs...)
}
