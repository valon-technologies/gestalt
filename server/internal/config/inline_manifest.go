package config

import (
	"fmt"

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
			MCP:                  p.MCP,
			OpenAPI:              p.OpenAPI,
			GraphQLURL:           p.GraphQLURL,
			MCPURL:               p.MCPURL,
			OpenAPIConnection:    p.OpenAPIConnection,
			GraphQLConnection:    p.GraphQLConnection,
			MCPConnection:        p.MCPConnection,
			DefaultConnection:    p.DefaultConnection,
			PostConnectDiscovery: p.PostConnectDiscovery,
		},
	}

	if p.ResponseMapping != nil {
		rm := &pluginmanifestv1.ManifestResponseMapping{
			DataPath: p.ResponseMapping.DataPath,
		}
		if p.ResponseMapping.Pagination != nil {
			rm.Pagination = &pluginmanifestv1.ManifestPaginationMapping{
				HasMorePath: p.ResponseMapping.Pagination.HasMorePath,
				CursorPath:  p.ResponseMapping.Pagination.CursorPath,
			}
		}
		manifest.Provider.ResponseMapping = rm
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

	if len(p.Operations) > 0 {
		manifest.Provider.Operations = make([]pluginmanifestv1.ProviderOperation, len(p.Operations))
		for i, op := range p.Operations {
			mop := pluginmanifestv1.ProviderOperation{
				Name:        op.Name,
				Description: op.Description,
				Method:      op.Method,
				Path:        op.Path,
			}
			if len(op.Parameters) > 0 {
				mop.Parameters = make([]pluginmanifestv1.ProviderParameter, len(op.Parameters))
				for j, param := range op.Parameters {
					mop.Parameters[j] = pluginmanifestv1.ProviderParameter{
						Name:        param.Name,
						Type:        param.Type,
						In:          param.In,
						Description: param.Description,
						Required:    param.Required,
					}
				}
			}
			manifest.Provider.Operations[i] = mop
		}
	}

	if len(p.ConnectionParams) > 0 {
		manifest.Provider.ConnectionParams = make(map[string]pluginmanifestv1.ProviderConnectionParam, len(p.ConnectionParams))
		for k, v := range p.ConnectionParams {
			manifest.Provider.ConnectionParams[k] = pluginmanifestv1.ProviderConnectionParam{
				Required:    v.Required,
				Description: v.Description,
				From:        v.From,
			}
		}
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
		Scopes:              auth.Scopes,
		PKCE:                auth.PKCE,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		AuthorizationParams: auth.AuthorizationParams,
		TokenParams:         auth.TokenParams,
		RefreshParams:       auth.RefreshParams,
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
		TokenMetadata:       auth.TokenMetadata,
	}
	for _, cf := range auth.Credentials {
		pa.Credentials = append(pa.Credentials, pluginmanifestv1.CredentialField{
			Name:        cf.Name,
			Label:       cf.Label,
			Description: cf.Description,
			HelpURL:     cf.HelpURL,
		})
	}
	return pa
}
