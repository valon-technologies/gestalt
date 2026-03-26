package compiler

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	"github.com/valon-technologies/gestalt/internal/config"
	graphqlupstream "github.com/valon-technologies/gestalt/internal/graphql"
	"github.com/valon-technologies/gestalt/internal/openapi"
	"github.com/valon-technologies/gestalt/internal/provider"
)

type Result struct {
	Definition *provider.Definition
	Catalog    *catalog.Catalog
}

func Compile(ctx context.Context, name string, upstream config.UpstreamDef, preparedProviders map[string]string) (*Result, error) {
	def, err := loadDefinition(ctx, name, upstream, preparedProviders)
	if err != nil {
		return nil, err
	}
	if len(upstream.DefaultParams) > 0 {
		def.DefaultParams = upstream.DefaultParams
	}
	return &Result{
		Definition: def,
		Catalog:    provider.CatalogFromDefinition(def),
	}, nil
}

func LoadDefinition(ctx context.Context, name string, upstream config.UpstreamDef, preparedProviders map[string]string) (*provider.Definition, error) {
	return loadDefinition(ctx, name, upstream, preparedProviders)
}

func BuildProvider(ctx context.Context, name string, intg config.IntegrationDef, upstream config.UpstreamDef, preparedProviders map[string]string, opts ...provider.BuildOption) (core.Provider, error) {
	def, err := loadDefinition(ctx, name, upstream, preparedProviders)
	if err != nil {
		return nil, err
	}
	if len(upstream.DefaultParams) > 0 {
		def.DefaultParams = upstream.DefaultParams
	}
	return provider.Build(def, integrationWithUpstreamAuth(intg, upstream), map[string]string(upstream.AllowedOperations), opts...)
}

func loadDefinition(ctx context.Context, name string, upstream config.UpstreamDef, preparedProviders map[string]string) (*provider.Definition, error) {
	if preparedPath := preparedProviders[name]; preparedPath != "" {
		return provider.LoadFile(preparedPath)
	}

	switch upstream.Type {
	case config.UpstreamTypeREST:
		if upstream.URL != "" {
			return openapi.LoadDefinition(ctx, name, upstream.URL, map[string]string(upstream.AllowedOperations))
		}
	case config.UpstreamTypeGraphQL:
		if upstream.URL != "" {
			return graphqlupstream.LoadDefinition(ctx, name, upstream.URL, map[string]string(upstream.AllowedOperations))
		}
	default:
		return nil, fmt.Errorf("unsupported api upstream type %q", upstream.Type)
	}

	return nil, fmt.Errorf("api upstream %q requires a url or prepared artifact", name)
}

func integrationWithUpstreamAuth(intg config.IntegrationDef, upstream config.UpstreamDef) config.IntegrationDef {
	if upstream.Auth.Type != "" {
		intg.Auth = upstream.Auth
	}
	if upstream.ClientID != "" {
		intg.ClientID = upstream.ClientID
	}
	if upstream.ClientSecret != "" {
		intg.ClientSecret = upstream.ClientSecret
	}
	if upstream.RedirectURL != "" {
		intg.RedirectURL = upstream.RedirectURL
	}
	return intg
}
