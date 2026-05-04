package bootstrap

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
)

type externalCredentialRefreshRuntimeConfig struct {
	Targets []externalCredentialRefreshRuntimeTarget `json:"targets" yaml:"targets"`
}

type externalCredentialRefreshRuntimeTarget struct {
	Provider            string                           `json:"provider" yaml:"provider"`
	Connection          string                           `json:"connection" yaml:"connection"`
	ConnectionID        string                           `json:"connectionId" yaml:"connectionId"`
	RefreshInterval     string                           `json:"refreshInterval" yaml:"refreshInterval"`
	RefreshBeforeExpiry string                           `json:"refreshBeforeExpiry" yaml:"refreshBeforeExpiry"`
	Auth                externalCredentialAuthConfigYAML `json:"auth" yaml:"auth"`
	ConnectionParams    map[string]string                `json:"connectionParams,omitempty" yaml:"connectionParams,omitempty"`
}

type externalCredentialAuthConfigYAML struct {
	Type                 string                                      `json:"type,omitempty" yaml:"type,omitempty"`
	Token                string                                      `json:"token,omitempty" yaml:"token,omitempty"`
	TokenPrefix          string                                      `json:"tokenPrefix,omitempty" yaml:"tokenPrefix,omitempty"`
	GrantType            string                                      `json:"grantType,omitempty" yaml:"grantType,omitempty"`
	TokenURL             string                                      `json:"tokenUrl,omitempty" yaml:"tokenUrl,omitempty"`
	ClientID             string                                      `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	ClientSecret         string                                      `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty"`
	ClientAuth           string                                      `json:"clientAuth,omitempty" yaml:"clientAuth,omitempty"`
	TokenExchange        string                                      `json:"tokenExchange,omitempty" yaml:"tokenExchange,omitempty"`
	Scopes               []string                                    `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	ScopeParam           string                                      `json:"scopeParam,omitempty" yaml:"scopeParam,omitempty"`
	ScopeSeparator       string                                      `json:"scopeSeparator,omitempty" yaml:"scopeSeparator,omitempty"`
	TokenParams          map[string]string                           `json:"tokenParams,omitempty" yaml:"tokenParams,omitempty"`
	RefreshParams        map[string]string                           `json:"refreshParams,omitempty" yaml:"refreshParams,omitempty"`
	AcceptHeader         string                                      `json:"acceptHeader,omitempty" yaml:"acceptHeader,omitempty"`
	AccessTokenPath      string                                      `json:"accessTokenPath,omitempty" yaml:"accessTokenPath,omitempty"`
	TokenExchangeDrivers []externalCredentialTokenExchangeDriverYAML `json:"tokenExchangeDrivers,omitempty" yaml:"tokenExchangeDrivers,omitempty"`
}

type externalCredentialTokenExchangeDriverYAML struct {
	Type            string            `json:"type,omitempty" yaml:"type,omitempty"`
	TargetPrincipal string            `json:"targetPrincipal,omitempty" yaml:"targetPrincipal,omitempty"`
	Scopes          []string          `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	LifetimeSeconds int               `json:"lifetimeSeconds,omitempty" yaml:"lifetimeSeconds,omitempty"`
	Endpoint        string            `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Params          map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
}

func buildExternalCredentialRefreshRuntimeTargets(cfg *config.Config) ([]externalCredentialRefreshRuntimeTarget, error) {
	if cfg == nil {
		return nil, nil
	}
	egressDeps := newEgressDeps(cfg)
	targets := make([]externalCredentialRefreshRuntimeTarget, 0)
	addProvider := func(kind, providerName string, entry *config.ProviderEntry) error {
		if entry == nil {
			return nil
		}
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return fmt.Errorf("%s name is empty", kind)
		}
		plan, err := config.BuildStaticConnectionPlan(entry, entry.ManifestSpec())
		if err != nil {
			return fmt.Errorf("%s %q: %w", kind, providerName, err)
		}
		policy := egressDeps.ProviderPolicy(entry)
		addConnection := func(connectionName string, resolved config.ResolvedConnectionDef) error {
			if resolved.CredentialRefresh == nil {
				return nil
			}
			conn := resolved.ConnectionDef()
			info, err := staticConnectionRuntimeInfo(providerName, connectionName, conn, policy)
			if err != nil {
				return fmt.Errorf("%s %q connection %q: %w", kind, providerName, connectionName, err)
			}
			if info.Mode != core.ConnectionModeUser {
				return fmt.Errorf("%s %q connection %q credentialRefresh requires mode user", kind, providerName, connectionName)
			}
			if strings.TrimSpace(info.AuthConfig.Type) != "oauth2" {
				return fmt.Errorf("%s %q connection %q credentialRefresh requires auth.type oauth2", kind, providerName, connectionName)
			}
			if strings.TrimSpace(info.AuthConfig.GrantType) == "client_credentials" {
				return fmt.Errorf("%s %q connection %q credentialRefresh does not support oauth2 client_credentials", kind, providerName, connectionName)
			}
			if strings.TrimSpace(info.AuthConfig.TokenURL) == "" {
				return fmt.Errorf("%s %q connection %q credentialRefresh requires auth.tokenUrl", kind, providerName, connectionName)
			}
			interval, before, err := credentialRefreshDurations(resolved.CredentialRefresh)
			if err != nil {
				return fmt.Errorf("%s %q connection %q credentialRefresh: %w", kind, providerName, connectionName, err)
			}
			connectionID := strings.TrimSpace(info.ConnectionID)
			if connectionID == "" {
				connectionID = providerName + ":" + connectionName
			}
			targets = append(targets, externalCredentialRefreshRuntimeTarget{
				Provider:            providerName,
				Connection:          connectionName,
				ConnectionID:        connectionID,
				RefreshInterval:     interval.String(),
				RefreshBeforeExpiry: before.String(),
				Auth:                externalCredentialAuthConfigToYAML(info.AuthConfig),
				ConnectionParams:    maps.Clone(info.Params),
			})
			return nil
		}
		if err := addConnection(config.PluginConnectionName, plan.ResolvedPluginConnection()); err != nil {
			return err
		}
		for _, connectionName := range plan.NamedConnectionNames() {
			resolved, _ := plan.ResolvedNamedConnectionDef(connectionName)
			if err := addConnection(connectionName, resolved); err != nil {
				return err
			}
		}
		return nil
	}
	for providerName, entry := range cfg.Plugins {
		if err := addProvider("integration", providerName, entry); err != nil {
			return nil, err
		}
	}
	for providerName, entry := range cfg.Providers.Agent {
		if err := addProvider("agent provider", providerName, entry); err != nil {
			return nil, err
		}
	}
	return dedupeExternalCredentialRefreshTargets(targets)
}

func credentialRefreshDurations(refresh *config.CredentialRefreshDef) (time.Duration, time.Duration, error) {
	if refresh == nil {
		return 0, 0, fmt.Errorf("config is required")
	}
	interval, err := config.ParseDuration(strings.TrimSpace(refresh.RefreshInterval))
	if err != nil {
		return 0, 0, fmt.Errorf("refreshInterval: %w", err)
	}
	before, err := config.ParseDuration(strings.TrimSpace(refresh.RefreshBeforeExpiry))
	if err != nil {
		return 0, 0, fmt.Errorf("refreshBeforeExpiry: %w", err)
	}
	return interval, before, nil
}

func dedupeExternalCredentialRefreshTargets(targets []externalCredentialRefreshRuntimeTarget) ([]externalCredentialRefreshRuntimeTarget, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	seen := make(map[string]externalCredentialRefreshRuntimeTarget, len(targets))
	out := make([]externalCredentialRefreshRuntimeTarget, 0, len(targets))
	for i := range targets {
		target := targets[i]
		connectionID := strings.TrimSpace(target.ConnectionID)
		if connectionID == "" {
			return nil, fmt.Errorf("credentialRefresh target for %s/%s has empty connectionId", target.Provider, target.Connection)
		}
		if existing, ok := seen[connectionID]; ok {
			if !reflect.DeepEqual(refreshConflictKey(existing), refreshConflictKey(target)) {
				return nil, fmt.Errorf("credentialRefresh target connectionId %q has conflicting refresh configuration", connectionID)
			}
			continue
		}
		seen[connectionID] = target
		out = append(out, target)
	}
	return out, nil
}

type externalCredentialRefreshConflictKey struct {
	RefreshInterval     string
	RefreshBeforeExpiry string
	Auth                externalCredentialRefreshAuthConflictKey
	ConnectionParams    map[string]string
}

type externalCredentialRefreshAuthConflictKey struct {
	Type            string
	TokenURL        string
	ClientID        string
	ClientSecret    string
	ClientAuth      string
	TokenExchange   string
	RefreshParams   map[string]string
	AcceptHeader    string
	AccessTokenPath string
}

func refreshConflictKey(target externalCredentialRefreshRuntimeTarget) externalCredentialRefreshConflictKey {
	return externalCredentialRefreshConflictKey{
		RefreshInterval:     target.RefreshInterval,
		RefreshBeforeExpiry: target.RefreshBeforeExpiry,
		Auth: externalCredentialRefreshAuthConflictKey{
			Type:            target.Auth.Type,
			TokenURL:        target.Auth.TokenURL,
			ClientID:        target.Auth.ClientID,
			ClientSecret:    target.Auth.ClientSecret,
			ClientAuth:      target.Auth.ClientAuth,
			TokenExchange:   target.Auth.TokenExchange,
			RefreshParams:   maps.Clone(target.Auth.RefreshParams),
			AcceptHeader:    target.Auth.AcceptHeader,
			AccessTokenPath: target.Auth.AccessTokenPath,
		},
		ConnectionParams: maps.Clone(target.ConnectionParams),
	}
}

func externalCredentialAuthConfigToYAML(auth core.ExternalCredentialAuthConfig) externalCredentialAuthConfigYAML {
	drivers := make([]externalCredentialTokenExchangeDriverYAML, 0, len(auth.TokenExchangeDrivers))
	for _, driver := range auth.TokenExchangeDrivers {
		drivers = append(drivers, externalCredentialTokenExchangeDriverYAML{
			Type:            driver.Type,
			TargetPrincipal: driver.TargetPrincipal,
			Scopes:          slices.Clone(driver.Scopes),
			LifetimeSeconds: driver.LifetimeSeconds,
			Endpoint:        driver.Endpoint,
			Params:          maps.Clone(driver.Params),
		})
	}
	return externalCredentialAuthConfigYAML{
		Type:                 auth.Type,
		Token:                auth.Token,
		TokenPrefix:          auth.TokenPrefix,
		GrantType:            auth.GrantType,
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
