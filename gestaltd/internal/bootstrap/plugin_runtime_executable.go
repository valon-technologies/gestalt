package bootstrap

import (
	"context"
	"fmt"
	"maps"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"gopkg.in/yaml.v3"
)

func buildExecutablePluginRuntime(ctx context.Context, name string, entry *config.RuntimeProviderEntry, deps Deps) (pluginruntime.Provider, error) {
	if entry == nil {
		return nil, fmt.Errorf("runtime provider entry is required")
	}

	runtimeConfig, err := runtimeProviderConfigMap(name, entry)
	if err != nil {
		return nil, err
	}

	return pluginruntime.NewExecutableProvider(ctx, pluginruntime.ExecutableConfig{
		Name:         name,
		Command:      entry.Command,
		Args:         append([]string(nil), entry.Args...),
		Env:          maps.Clone(entry.Env),
		Config:       runtimeConfig,
		AllowedHosts: append([]string(nil), entry.AllowedHosts...),
		HostBinary:   entry.HostBinary,
		Telemetry:    deps.Telemetry,
	})
}

func runtimeProviderConfigMap(name string, entry *config.RuntimeProviderEntry) (map[string]any, error) {
	if entry == nil {
		return nil, fmt.Errorf("runtime provider entry is required")
	}

	configNode := entry.Config
	if config.IsComponentRuntimeConfigNode(configNode) {
		var wrapped struct {
			Config yaml.Node `yaml:"config,omitempty"`
		}
		if err := configNode.Decode(&wrapped); err != nil {
			return nil, fmt.Errorf("decode wrapped runtime provider %q config: %w", name, err)
		}
		configNode = wrapped.Config
	}

	runtimeConfig := map[string]any{}
	if configNode.Kind == 0 {
		return runtimeConfig, nil
	}
	if err := configNode.Decode(&runtimeConfig); err != nil {
		return nil, fmt.Errorf("decode runtime provider %q config: %w", name, err)
	}
	return runtimeConfig, nil
}
