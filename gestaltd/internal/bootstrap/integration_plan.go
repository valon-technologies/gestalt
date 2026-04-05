package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

type compiledIntegration struct {
	name                    string
	intg                    config.IntegrationDef
	meta                    providerMetadata
	pluginConfig            map[string]any
	hasExecutableEntrypoint bool

	manifest          *pluginmanifestv1.Manifest
	manifestProvider  *pluginmanifestv1.Provider
	allowedOperations map[string]*config.OperationOverride
	connectionPlan    pluginConnectionPlan
	specOverlay       provider.DefinitionOverlay
}

func compileIntegration(name string, intg config.IntegrationDef) (*compiledIntegration, error) {
	if intg.Plugin == nil {
		return nil, fmt.Errorf("integration %q has no plugin defined", name)
	}

	meta := resolveProviderMetadata(intg)
	pluginConfig, err := config.NodeToMap(intg.Plugin.Config)
	if err != nil {
		return nil, fmt.Errorf("decode plugin config for %q: %w", name, err)
	}

	compiled := &compiledIntegration{
		name:                    name,
		intg:                    intg,
		meta:                    meta,
		pluginConfig:            pluginConfig,
		hasExecutableEntrypoint: !intg.Plugin.IsInline() && !intg.Plugin.IsDeclarative,
	}

	if compiled.hasExecutableEntrypoint {
		if err := compiled.compileExecutable(); err != nil {
			return nil, err
		}
		return compiled, nil
	}

	if err := compiled.compileManifestBacked(); err != nil {
		return nil, err
	}
	return compiled, nil
}

func (compiled *compiledIntegration) compileExecutable() error {
	compiled.manifest = compiled.intg.Plugin.ResolvedManifest
	compiled.manifestProvider = compiled.intg.Plugin.ManifestProvider()
	compiled.allowedOperations = compiled.intg.Plugin.AllowedOperations

	specPlugin := compiled.intg.Plugin
	if compiled.intg.Plugin.HasResolvedManifest() {
		resolvedManifest, resolvedAllowedOperations, err := config.EffectiveManifestBackedInputs(compiled.name, compiled.intg.Plugin)
		if err != nil {
			return err
		}
		compiled.manifest = resolvedManifest
		if resolvedManifest != nil {
			compiled.manifestProvider = resolvedManifest.Provider
		}
		compiled.allowedOperations = resolvedAllowedOperations
		specPlugin = nil
	}

	plan, err := buildPluginConnectionPlan(compiled.intg.Plugin, compiled.manifestProvider)
	if err != nil {
		return fmt.Errorf("resolve connections for %q: %w", compiled.name, err)
	}
	compiled.connectionPlan = plan
	compiled.specOverlay = provider.NewDefinitionOverlay(compiled.manifestProvider, specPlugin, compiled.meta.displayName, compiled.meta.description, compiled.meta.iconSVG)
	return nil
}

func (compiled *compiledIntegration) compileManifestBacked() error {
	manifest, allowedOperations, err := config.EffectiveManifestBackedInputs(compiled.name, compiled.intg.Plugin)
	if err != nil {
		return err
	}
	if manifest == nil || manifest.Provider == nil {
		return fmt.Errorf("declarative provider %q has no resolved manifest", compiled.name)
	}

	plan, err := buildPluginConnectionPlan(compiled.intg.Plugin, manifest.Provider)
	if err != nil {
		return fmt.Errorf("resolve connections for %q: %w", compiled.name, err)
	}

	compiled.manifest = manifest
	compiled.manifestProvider = manifest.Provider
	compiled.allowedOperations = allowedOperations
	compiled.connectionPlan = plan
	compiled.specOverlay = provider.NewDefinitionOverlay(compiled.manifestProvider, nil, compiled.meta.displayName, compiled.meta.description, compiled.meta.iconSVG)
	return nil
}

func (compiled *compiledIntegration) newProviderBuildResult(prov core.Provider, authFallback *specAuthFallback, deps Deps, regStore *lazyRegStore) (*ProviderBuildResult, error) {
	result := &ProviderBuildResult{
		Provider:          prov,
		DefaultConnection: compiled.connectionPlan.authDefaultConnection(),
		APIConnection:     compiled.connectionPlan.apiConnection(),
		MCPConnection:     compiled.connectionPlan.mcpConnection(),
	}
	var err error
	result.ConnectionAuth, err = buildConnectionAuthMap(compiled, authFallback, deps, regStore)
	if err != nil {
		closeIfPossible(prov)
		return nil, err
	}
	return result, nil
}

func (compiled *compiledIntegration) mcpURL() string {
	if resolved, ok := compiled.connectionPlan.resolvedSurface(config.SpecSurfaceMCP); ok {
		return resolved.url
	}
	return ""
}
