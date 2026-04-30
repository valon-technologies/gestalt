package providerhost

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type ProcessConfig = runtimehost.ProcessConfig
type HostService = runtimehost.HostService
type PluginProcess = runtimehost.PluginProcess

type ExecConfig struct {
	Command          string
	Args             []string
	Env              map[string]string
	StaticSpec       StaticProviderSpec
	Config           map[string]any
	Egress           egress.Policy
	HostBinary       string
	Cleanup          func()
	HostServices     []HostService
	InvocationTokens *InvocationTokenManager
	InvocationGrants invocationGrants
	ProviderName     string
	Telemetry        metricutil.TelemetryProviders
}

func NewExecutableProvider(ctx context.Context, cfg ExecConfig) (core.Provider, error) {
	return pluginservice.NewExecutable(ctx, pluginservice.ExecConfig{
		Command:          cfg.Command,
		Args:             cfg.Args,
		Env:              cfg.Env,
		StaticSpec:       cfg.StaticSpec,
		Config:           cfg.Config,
		Egress:           cfg.Egress,
		HostBinary:       cfg.HostBinary,
		Cleanup:          cfg.Cleanup,
		HostServices:     cfg.HostServices,
		InvocationTokens: cfg.InvocationTokens,
		InvocationGrants: cfg.InvocationGrants,
		ProviderName:     cfg.ProviderName,
		Telemetry:        cfg.Telemetry,
	})
}

func StartPluginProcess(ctx context.Context, cfg ProcessConfig) (*PluginProcess, error) {
	return runtimehost.StartPluginProcess(ctx, cfg)
}

func NewPluginTempDir(pattern string) (string, error) {
	return runtimehost.NewPluginTempDir(pattern)
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return pluginservice.ServeProvider(ctx, provider)
}
