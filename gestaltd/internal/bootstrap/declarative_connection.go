package bootstrap

import (
	"maps"
	"slices"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/maputil"
	"github.com/valon-technologies/gestalt/server/services/apps/declarative"
)

func declarativeConnectionDef(conn config.ConnectionDef) declarative.ConnectionDef {
	return declarative.ConnectionDef{
		Mode:             conn.Mode,
		Auth:             declarativeConnectionAuthDef(conn.Auth),
		ConnectionParams: declarativeConnectionParamDefs(conn.ConnectionParams),
	}
}

func declarativeNamedConnectionDef(_ string, conn config.ConnectionDef) declarative.ConnectionDef {
	return declarativeConnectionDef(conn)
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
	return maputil.ConvertNonEmpty(params, func(value config.ConnectionParamDef) declarative.ConnectionParamDef {
		return declarative.ConnectionParamDef(value)
	})
}
