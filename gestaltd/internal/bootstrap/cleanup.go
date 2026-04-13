package bootstrap

import (
	"errors"
	"fmt"
	"io"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/registry"
)

// CloseProviders closes all registered providers that implement io.Closer.
func CloseProviders(providers *registry.ProviderMap[core.Provider]) error {
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
