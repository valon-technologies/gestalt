package env

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type Provider struct {
	prefix string
}

func (p *Provider) GetSecret(_ context.Context, name string) (string, error) {
	envName := p.prefix + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	val, ok := os.LookupEnv(envName)
	if !ok {
		return "", fmt.Errorf("%w: environment variable %q not set", core.ErrSecretNotFound, envName)
	}
	return val, nil
}
