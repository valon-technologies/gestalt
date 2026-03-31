package compiler

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/config"
	graphqlupstream "github.com/valon-technologies/gestalt/server/internal/graphql"
	"github.com/valon-technologies/gestalt/server/internal/openapi"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

type Result struct {
	Definition *provider.Definition
	Catalog    *catalog.Catalog
}

func Compile(ctx context.Context, name string, api config.APIDef, preparedProviders map[string]string) (*Result, error) {
	def, err := loadDefinition(ctx, name, api, preparedProviders)
	if err != nil {
		return nil, err
	}
	return &Result{
		Definition: def,
		Catalog:    provider.CatalogFromDefinition(def),
	}, nil
}

func LoadDefinition(ctx context.Context, name string, api config.APIDef, preparedProviders map[string]string) (*provider.Definition, error) {
	return loadDefinition(ctx, name, api, preparedProviders)
}

func BuildProvider(ctx context.Context, name string, intg config.IntegrationDef, api config.APIDef, conn config.ConnectionDef, preparedProviders map[string]string, allowedOps map[string]*config.OperationOverride, opts ...provider.BuildOption) (core.Provider, error) {
	def, err := loadDefinition(ctx, name, api, preparedProviders)
	if err != nil {
		return nil, err
	}
	provider.ApplyDisplayOverrides(def, intg)
	provider.ApplyResponseMapping(def, api)
	return provider.Build(def, conn, allowedOps, opts...)
}

func loadDefinition(ctx context.Context, name string, api config.APIDef, preparedProviders map[string]string) (*provider.Definition, error) {
	if preparedPath := preparedProviders[name]; preparedPath != "" {
		return provider.LoadFile(preparedPath)
	}

	switch api.Type {
	case config.APITypeREST:
		if api.OpenAPI != "" {
			return openapi.LoadDefinition(ctx, name, api.OpenAPI, nil)
		}
	case config.APITypeGraphQL:
		if api.URL != "" {
			return graphqlupstream.LoadDefinition(ctx, name, api.URL, nil)
		}
	default:
		return nil, fmt.Errorf("unsupported api type %q", api.Type)
	}

	return nil, fmt.Errorf("api %q requires a url/openapi or prepared artifact", name)
}
