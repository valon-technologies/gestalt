package extend

import (
	"context"
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
)

//go:embed provider.yaml
var definitionYAML []byte

func Factory(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (core.Provider, error) {
	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		return nil, fmt.Errorf("extend: parsing embedded definition: %w", err)
	}
	return provider.Build(&def, intg, nil, provider.WithEgressResolver(deps.Egress.Resolver))
}
