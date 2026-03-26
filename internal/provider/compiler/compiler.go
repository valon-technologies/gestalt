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

// Result is the normalized compiler output for a remote upstream or prepared
// provider artifact.
type Result struct {
	Definition *provider.Definition
	Catalog    *catalog.Catalog
}

// Compile normalizes a remote upstream or prepared provider artifact into the
// definition and catalog forms used by the rest of Gestalt.
func Compile(ctx context.Context, name string, upstream config.UpstreamDef, preparedProviders map[string]string) (*Result, error) {
	def, err := loadDefinition(ctx, name, upstream, preparedProviders)
	if err != nil {
		return nil, err
	}
	return &Result{
		Definition: def,
		Catalog:    provider.CatalogFromDefinition(def),
	}, nil
}

// LoadDefinition exposes the compiler's definition output for callers that only
// need the provider artifact representation.
func LoadDefinition(ctx context.Context, name string, upstream config.UpstreamDef, preparedProviders map[string]string) (*provider.Definition, error) {
	result, err := Compile(ctx, name, upstream, preparedProviders)
	if err != nil {
		return nil, err
	}
	return result.Definition, nil
}

// BuildProvider constructs the runtime provider for a REST or GraphQL upstream.
func BuildProvider(ctx context.Context, name string, intg config.IntegrationDef, upstream config.UpstreamDef, preparedProviders map[string]string, opts ...provider.BuildOption) (core.Provider, error) {
	def, err := LoadDefinition(ctx, name, upstream, preparedProviders)
	if err != nil {
		return nil, err
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
