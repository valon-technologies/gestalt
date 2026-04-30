package bootstrap

import (
	"context"
	"fmt"
	"maps"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/pluginruntime"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
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

	var sessionLogs runtimelogs.Store
	if deps.Services != nil {
		sessionLogs = deps.Services.RuntimeSessionLogs
	}

	return pluginruntime.NewExecutableProvider(ctx, pluginruntime.ExecutableConfig{
		Name:         name,
		Command:      entry.Command,
		Args:         append([]string(nil), entry.Args...),
		Env:          maps.Clone(entry.Env),
		Config:       runtimeConfig,
		Egress:       deps.Egress.Policy(entry.EffectiveAllowedHosts()),
		HostBinary:   entry.HostBinary,
		HostServices: buildRuntimeProviderHostServices(name, deps),
		Telemetry:    deps.Telemetry,
		SessionLogs:  sessionLogs,
	})
}

func buildRuntimeProviderHostServices(name string, deps Deps) []runtimehost.HostService {
	if deps.Services == nil || deps.Services.RuntimeSessionLogs == nil {
		return nil
	}
	return []runtimehost.HostService{{
		Name:   "runtime_log_host",
		EnvVar: runtimehost.DefaultRuntimeLogHostSocketEnv,
		Register: func(srv *grpc.Server) {
			runtimehost.RegisterRuntimeLogHostServer(srv, name, deps.Services.RuntimeSessionLogs.AppendSessionLogs)
		},
	}}
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
