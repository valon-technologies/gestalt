package jira

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"time"

	"github.com/valon-technologies/gestalt/internal/bootstrap"
	"github.com/valon-technologies/gestalt/internal/config"
	"github.com/valon-technologies/gestalt/internal/provider"
	"gopkg.in/yaml.v3"
)

//go:embed provider.yaml
var definitionYAML []byte

func Factory(ctx context.Context, name string, intg config.IntegrationDef, deps bootstrap.Deps) (*bootstrap.ProviderBuildResult, error) {
	var def provider.Definition
	if err := yaml.Unmarshal(definitionYAML, &def); err != nil {
		return nil, err
	}
	conn, err := bootstrap.ResolveAPIConnection(intg)
	if err != nil {
		return nil, fmt.Errorf("jira: %w", err)
	}
	provider.ApplyDisplayOverrides(&def, intg)
	provider.ApplyConnectionAuth(&def, conn)
	prov, err := provider.Build(&def, conn, nil, provider.WithEgressResolver(deps.Egress.Resolver))
	if err != nil {
		return nil, err
	}
	result := &bootstrap.ProviderBuildResult{Provider: prov}
	if conn.Auth.Type != "manual" && conn.Auth.Type != "api_key" {
		upstream, err := provider.BuildOAuthUpstream(&def, conn, def.BaseURL, &http.Client{Timeout: 10 * time.Second})
		if err == nil {
			connName := intg.API.Connection
			result.ConnectionAuth = map[string]bootstrap.OAuthHandler{
				connName: bootstrap.WrapUpstreamHandler(upstream),
			}
		}
	}
	return result, nil
}
