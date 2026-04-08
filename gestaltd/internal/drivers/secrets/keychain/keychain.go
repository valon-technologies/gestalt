package keychain

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/zalando/go-keyring"
)

type Provider struct {
	service string
}

func (p *Provider) GetSecret(_ context.Context, name string) (string, error) {
	val, err := keyring.Get(p.service, name)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", fmt.Errorf("%w: keychain entry %q not found in service %q", core.ErrSecretNotFound, name, p.service)
		}
		return "", fmt.Errorf("keychain lookup for %q: %w", name, err)
	}
	return val, nil
}
