package config

import (
	"fmt"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func InlineToManifest(name string, p *PluginDef) (*pluginmanifestv1.Manifest, error) {
	if p == nil {
		return nil, fmt.Errorf("nil plugin definition")
	}
	if !p.IsInline() {
		return nil, fmt.Errorf("plugin %q is not inline", name)
	}

	manifest := &pluginmanifestv1.Manifest{
		DisplayName: name,
		Kinds:       []string{pluginmanifestv1.KindProvider},
		Provider:    &pluginmanifestv1.Provider{},
	}

	if p.BaseURL != "" {
		manifest.Provider.BaseURL = p.BaseURL
	}
	if p.MCP {
		manifest.Provider.MCP = true
	}

	if p.Auth != nil {
		manifest.Provider.Auth = inlineAuthToManifest(p.Auth)
	}

	for _, op := range p.Operations {
		mop := pluginmanifestv1.ProviderOperation{
			Name:        op.Name,
			Description: op.Description,
			Method:      op.Method,
			Path:        op.Path,
		}
		for _, param := range op.Parameters {
			mop.Parameters = append(mop.Parameters, pluginmanifestv1.ProviderParameter{
				Name:        param.Name,
				Type:        param.Type,
				In:          param.In,
				Description: param.Description,
				Required:    param.Required,
			})
		}
		manifest.Provider.Operations = append(manifest.Provider.Operations, mop)
	}

	if p.OpenAPI != "" {
		manifest.Provider.OpenAPI = p.OpenAPI
	}
	if p.GraphQLURL != "" {
		manifest.Provider.GraphQLURL = p.GraphQLURL
	}
	if p.MCPURL != "" {
		manifest.Provider.MCPURL = p.MCPURL
	}
	if p.OpenAPIConnection != "" {
		manifest.Provider.OpenAPIConnection = p.OpenAPIConnection
	}
	if p.GraphQLConnection != "" {
		manifest.Provider.GraphQLConnection = p.GraphQLConnection
	}
	if p.MCPConnection != "" {
		manifest.Provider.MCPConnection = p.MCPConnection
	}

	if len(p.AllowedOperations) > 0 {
		manifest.Provider.AllowedOperations = make(map[string]*pluginmanifestv1.ProviderOperationOverride, len(p.AllowedOperations))
		for k, v := range p.AllowedOperations {
			if v != nil {
				manifest.Provider.AllowedOperations[k] = &pluginmanifestv1.ProviderOperationOverride{
					Alias:       v.Alias,
					Description: v.Description,
				}
			} else {
				manifest.Provider.AllowedOperations[k] = nil
			}
		}
	}

	if len(p.Connections) > 0 {
		manifest.Provider.Connections = make(map[string]*pluginmanifestv1.ProviderConnectionDef, len(p.Connections))
		for k, v := range p.Connections {
			if v != nil {
				cd := &pluginmanifestv1.ProviderConnectionDef{
					Mode: v.Mode,
					Auth: inlineAuthToManifest(v.Auth),
				}
				if len(v.Params) > 0 {
					cd.Params = make(map[string]pluginmanifestv1.ProviderConnectionParam, len(v.Params))
					for pk, pv := range v.Params {
						cd.Params[pk] = pluginmanifestv1.ProviderConnectionParam{
							Required: pv.Required,
						}
					}
				}
				manifest.Provider.Connections[k] = cd
			}
		}
	}

	if len(p.ConnectionParams) > 0 {
		manifest.Provider.Connection = make(map[string]pluginmanifestv1.ProviderConnectionParam)
		for k, v := range p.ConnectionParams {
			manifest.Provider.Connection[k] = pluginmanifestv1.ProviderConnectionParam{
				Required: v.Required,
			}
		}
	}

	return manifest, nil
}

func inlineAuthToManifest(auth *InlineAuthDef) *pluginmanifestv1.ProviderAuth {
	if auth == nil {
		return nil
	}
	pa := &pluginmanifestv1.ProviderAuth{
		Type:                auth.Type,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		Scopes:              auth.Scopes,
		PKCE:                auth.PKCE,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		AuthorizationParams: auth.AuthorizationParams,
	}
	for _, cred := range auth.Credentials {
		pa.Credentials = append(pa.Credentials, pluginmanifestv1.CredentialField{
			Name:        cred.Name,
			Label:       cred.Label,
			Description: cred.Description,
			HelpURL:     cred.HelpURL,
		})
	}
	return pa
}
