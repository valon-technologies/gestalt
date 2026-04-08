package config

import (
	"fmt"
	"maps"
	"slices"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func InlineToManifest(name string, p *PluginDef) (*pluginmanifestv1.Manifest, error) {
	if p == nil {
		return nil, fmt.Errorf("nil plugin def")
	}

	manifest := &pluginmanifestv1.Manifest{
		DisplayName: name,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider: &pluginmanifestv1.Provider{
			BaseURL:              p.BaseURL,
			Headers:              NormalizeHeaders(p.Headers),
			ManagedParameters:    NormalizeManagedParameters(p.ManagedParameters),
			ConnectionMode:       p.ConnectionMode,
			MCP:                  p.MCP,
			OpenAPI:              p.OpenAPI,
			GraphQLURL:           p.GraphQLURL,
			MCPURL:               p.MCPURL,
			OpenAPIConnection:    p.OpenAPIConnection,
			GraphQLConnection:    p.GraphQLConnection,
			MCPConnection:        p.MCPConnection,
			DefaultConnection:    p.DefaultConnection,
			PostConnectDiscovery: p.PostConnectDiscovery,
			Operations:           cloneInlineOperations(p.Operations),
			ResponseMapping:      p.ResponseMapping,
			Pagination:           p.Pagination,
			ConnectionParams:     maps.Clone(p.ConnectionParams),
			AllowedOperations:    maps.Clone(p.AllowedOperations),
		},
	}

	if len(p.Connections) > 0 {
		manifest.Provider.Connections = make(map[string]*pluginmanifestv1.ManifestConnectionDef, len(p.Connections))
		for k, v := range p.Connections {
			if v == nil {
				manifest.Provider.Connections[k] = nil
				continue
			}
			mcd := &pluginmanifestv1.ManifestConnectionDef{Mode: v.Mode}
			if v.Auth.Type != "" {
				mcd.Auth = connectionAuthToManifest(&v.Auth)
			}
			manifest.Provider.Connections[k] = mcd
		}
	}

	if p.Auth != nil {
		manifest.Provider.Auth = connectionAuthToManifest(p.Auth)
	}

	return manifest, nil
}

func connectionAuthToManifest(auth *ConnectionAuthDef) *pluginmanifestv1.ProviderAuth {
	if auth == nil {
		return nil
	}
	pa := &pluginmanifestv1.ProviderAuth{
		Type:                auth.Type,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		ClientID:            auth.ClientID,
		ClientSecret:        auth.ClientSecret,
		Scopes:              slices.Clone(auth.Scopes),
		PKCE:                auth.PKCE,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		AuthorizationParams: maps.Clone(auth.AuthorizationParams),
		TokenParams:         maps.Clone(auth.TokenParams),
		RefreshParams:       maps.Clone(auth.RefreshParams),
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
		TokenMetadata:       slices.Clone(auth.TokenMetadata),
		Credentials:         slices.Clone(auth.Credentials),
	}
	return pa
}

func cloneInlineOperations(ops []InlineOperationDef) []pluginmanifestv1.ProviderOperation {
	if len(ops) == 0 {
		return nil
	}

	cloned := slices.Clone(ops)
	for i := range cloned {
		cloned[i].Parameters = slices.Clone(cloned[i].Parameters)
	}
	return cloned
}
