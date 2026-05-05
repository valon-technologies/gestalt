package bootstrap

import (
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
)

func declarativeConnectionDef(conn config.ConnectionDef) declarative.ConnectionDef {
	return declarative.ConnectionDef{
		Mode:             conn.Mode,
		Auth:             declarativeConnectionAuthDef(conn.Auth),
		ConnectionParams: declarativeConnectionParamDefs(conn.ConnectionParams),
	}
}

func declarativeNamedConnectionDef(name string, conn config.ConnectionDef) declarative.ConnectionDef {
	out := declarativeConnectionDef(conn)
	if postConnect := pluginservice.PostConnectConfigFromManifest(conn.PostConnect); postConnect != nil {
		out.PostConnect = map[string]*core.PostConnectConfig{name: postConnect}
	}
	return out
}

func declarativeConnectionAuthDef(auth config.ConnectionAuthDef) declarative.ConnectionAuthDef {
	return declarative.ConnectionAuthDef{
		Type:                auth.Type,
		Token:               auth.Token,
		GrantType:           auth.GrantType,
		RefreshToken:        auth.RefreshToken,
		AuthorizationURL:    auth.AuthorizationURL,
		TokenURL:            auth.TokenURL,
		ClientID:            auth.ClientID,
		ClientSecret:        auth.ClientSecret,
		RedirectURL:         auth.RedirectURL,
		ClientAuth:          auth.ClientAuth,
		TokenExchange:       auth.TokenExchange,
		TokenPrefix:         auth.TokenPrefix,
		Scopes:              slices.Clone(auth.Scopes),
		ScopeParam:          auth.ScopeParam,
		ScopeSeparator:      auth.ScopeSeparator,
		PKCE:                auth.PKCE,
		AuthorizationParams: maps.Clone(auth.AuthorizationParams),
		TokenParams:         maps.Clone(auth.TokenParams),
		RefreshParams:       maps.Clone(auth.RefreshParams),
		AcceptHeader:        auth.AcceptHeader,
		AccessTokenPath:     auth.AccessTokenPath,
		TokenMetadata:       slices.Clone(auth.TokenMetadata),
		Credentials:         slices.Clone(auth.Credentials),
		AuthMapping:         config.CloneAuthMapping(auth.AuthMapping),
	}
}

func declarativeConnectionParamDefs(params map[string]config.ConnectionParamDef) map[string]declarative.ConnectionParamDef {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]declarative.ConnectionParamDef, len(params))
	for name, param := range params {
		out[name] = declarative.ConnectionParamDef{
			Required:    param.Required,
			Description: param.Description,
			Default:     param.Default,
			From:        param.From,
			Field:       param.Field,
		}
	}
	return out
}
