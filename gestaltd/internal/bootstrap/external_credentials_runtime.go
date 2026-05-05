package bootstrap

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type externalCredentialsResolvedConnectionConfig struct {
	Provider          string                                      `yaml:"provider"`
	Connection        string                                      `yaml:"connection"`
	ConnectionID      string                                      `yaml:"connectionId"`
	Mode              string                                      `yaml:"mode"`
	Auth              externalCredentialsResolvedAuthConfig       `yaml:"auth"`
	ConnectionParams  map[string]string                           `yaml:"connectionParams,omitempty"`
	CredentialRefresh *externalCredentialsCredentialRefreshConfig `yaml:"credentialRefresh,omitempty"`
}

type externalCredentialsCredentialRefreshConfig struct {
	RefreshInterval     string `yaml:"refreshInterval"`
	RefreshBeforeExpiry string `yaml:"refreshBeforeExpiry"`
}

type externalCredentialsResolvedAuthConfig struct {
	Type                 string                                           `yaml:"type,omitempty"`
	Token                string                                           `yaml:"token,omitempty"`
	TokenPrefix          string                                           `yaml:"tokenPrefix,omitempty"`
	GrantType            string                                           `yaml:"grantType,omitempty"`
	RefreshToken         string                                           `yaml:"refreshToken,omitempty"`
	TokenURL             string                                           `yaml:"tokenUrl,omitempty"`
	ClientID             string                                           `yaml:"clientId,omitempty"`
	ClientSecret         string                                           `yaml:"clientSecret,omitempty"`
	ClientAuth           string                                           `yaml:"clientAuth,omitempty"`
	TokenExchange        string                                           `yaml:"tokenExchange,omitempty"`
	Scopes               []string                                         `yaml:"scopes,omitempty"`
	ScopeParam           string                                           `yaml:"scopeParam,omitempty"`
	ScopeSeparator       string                                           `yaml:"scopeSeparator,omitempty"`
	TokenParams          map[string]string                                `yaml:"tokenParams,omitempty"`
	RefreshParams        map[string]string                                `yaml:"refreshParams,omitempty"`
	AcceptHeader         string                                           `yaml:"acceptHeader,omitempty"`
	AccessTokenPath      string                                           `yaml:"accessTokenPath,omitempty"`
	TokenExchangeDrivers []externalCredentialsResolvedTokenExchangeDriver `yaml:"tokenExchangeDrivers,omitempty"`
}

type externalCredentialsResolvedTokenExchangeDriver struct {
	Type            string            `yaml:"type,omitempty"`
	TargetPrincipal string            `yaml:"targetPrincipal,omitempty"`
	Scopes          []string          `yaml:"scopes,omitempty"`
	LifetimeSeconds int               `yaml:"lifetimeSeconds,omitempty"`
	Endpoint        string            `yaml:"endpoint,omitempty"`
	Params          map[string]string `yaml:"params,omitempty"`
}

func buildExternalCredentialsResolvedConnections(cfg *config.Config) ([]externalCredentialsResolvedConnectionConfig, error) {
	runtime, err := BuildConnectionRuntime(cfg)
	if err != nil {
		return nil, err
	}
	if len(runtime) == 0 {
		return nil, nil
	}
	providers := make([]string, 0, len(runtime))
	for provider := range runtime {
		providers = append(providers, provider)
	}
	sort.Strings(providers)

	var out []externalCredentialsResolvedConnectionConfig
	hasCredentialRefresh := false
	for _, provider := range providers {
		connections := runtime[provider]
		names := make([]string, 0, len(connections))
		for name := range connections {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, connection := range names {
			info := connections[connection]
			refresh, err := externalCredentialsCredentialRefresh(info.CredentialRefresh)
			if err != nil {
				return nil, fmt.Errorf("%s/%s credentialRefresh: %w", provider, connection, err)
			}
			if refresh != nil {
				hasCredentialRefresh = true
			}
			out = append(out, externalCredentialsResolvedConnectionConfig{
				Provider:          provider,
				Connection:        connection,
				ConnectionID:      connectionRuntimeID(provider, connection, info),
				Mode:              string(info.Mode),
				Auth:              externalCredentialsResolvedAuth(info.AuthConfig),
				ConnectionParams:  maps.Clone(info.Params),
				CredentialRefresh: refresh,
			})
		}
	}
	if !hasCredentialRefresh {
		return nil, nil
	}
	return out, nil
}

func connectionRuntimeID(provider, connection string, info invocation.ConnectionRuntimeInfo) string {
	if id := strings.TrimSpace(info.ConnectionID); id != "" {
		return id
	}
	connection = strings.TrimSpace(connection)
	if connection == "" {
		connection = core.PluginConnectionName
	}
	return strings.TrimSpace(provider) + ":" + connection
}

func externalCredentialsCredentialRefresh(src *providermanifestv1.CredentialRefreshConfig) (*externalCredentialsCredentialRefreshConfig, error) {
	if src == nil {
		return nil, nil
	}
	refreshInterval, err := canonicalCredentialRefreshDuration(src.RefreshInterval)
	if err != nil {
		return nil, fmt.Errorf("refreshInterval: %w", err)
	}
	refreshBeforeExpiry, err := canonicalCredentialRefreshDuration(src.RefreshBeforeExpiry)
	if err != nil {
		return nil, fmt.Errorf("refreshBeforeExpiry: %w", err)
	}
	return &externalCredentialsCredentialRefreshConfig{
		RefreshInterval:     refreshInterval,
		RefreshBeforeExpiry: refreshBeforeExpiry,
	}, nil
}

func canonicalCredentialRefreshDuration(value string) (string, error) {
	duration, err := config.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return duration.String(), nil
}

func externalCredentialsResolvedAuth(auth core.ExternalCredentialAuthConfig) externalCredentialsResolvedAuthConfig {
	drivers := make([]externalCredentialsResolvedTokenExchangeDriver, 0, len(auth.TokenExchangeDrivers))
	for _, driver := range auth.TokenExchangeDrivers {
		drivers = append(drivers, externalCredentialsResolvedTokenExchangeDriver{
			Type:            strings.TrimSpace(driver.Type),
			TargetPrincipal: strings.TrimSpace(driver.TargetPrincipal),
			Scopes:          slices.Clone(driver.Scopes),
			LifetimeSeconds: driver.LifetimeSeconds,
			Endpoint:        strings.TrimSpace(driver.Endpoint),
			Params:          maps.Clone(driver.Params),
		})
	}
	return externalCredentialsResolvedAuthConfig{
		Type:                 auth.Type,
		Token:                auth.Token,
		TokenPrefix:          auth.TokenPrefix,
		GrantType:            auth.GrantType,
		RefreshToken:         auth.RefreshToken,
		TokenURL:             auth.TokenURL,
		ClientID:             auth.ClientID,
		ClientSecret:         auth.ClientSecret,
		ClientAuth:           auth.ClientAuth,
		TokenExchange:        auth.TokenExchange,
		Scopes:               slices.Clone(auth.Scopes),
		ScopeParam:           auth.ScopeParam,
		ScopeSeparator:       auth.ScopeSeparator,
		TokenParams:          maps.Clone(auth.TokenParams),
		RefreshParams:        maps.Clone(auth.RefreshParams),
		AcceptHeader:         auth.AcceptHeader,
		AccessTokenPath:      auth.AccessTokenPath,
		TokenExchangeDrivers: drivers,
	}
}
