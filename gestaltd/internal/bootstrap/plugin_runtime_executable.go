package bootstrap

import (
	"context"
	"fmt"
	"maps"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
)

func buildExecutablePluginRuntime(ctx context.Context, name string, entry *config.RuntimeProviderEntry, _ Deps) (pluginruntime.Provider, error) {
	if entry == nil {
		return nil, fmt.Errorf("runtime provider entry is required")
	}

	configNode := entry.Config
	if !config.IsComponentRuntimeConfigNode(configNode) {
		var err error
		configNode, err = config.BuildComponentRuntimeConfigNode(name, "runtime", &entry.ProviderEntry, entry.Config)
		if err != nil {
			return nil, fmt.Errorf("runtime provider %q: %w", name, err)
		}
	}

	runtimeConfig := map[string]any{}
	if configNode.Kind != 0 {
		if err := configNode.Decode(&runtimeConfig); err != nil {
			return nil, fmt.Errorf("decode runtime provider %q config: %w", name, err)
		}
	}

	return pluginruntime.NewExecutableProvider(ctx, pluginruntime.ExecutableConfig{
		Name:         name,
		Command:      entry.Command,
		Args:         append([]string(nil), entry.Args...),
		Env:          maps.Clone(entry.Env),
		Config:       runtimeConfig,
		AllowedHosts: append([]string(nil), entry.AllowedHosts...),
		HostBinary:   entry.HostBinary,
	})
}
