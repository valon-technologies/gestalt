package providerhost

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	pluginservice "github.com/valon-technologies/gestalt/server/services/plugins"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc"
)

type ProcessConfig = runtimehost.ProcessConfig
type HostService = runtimehost.HostService
type PluginProcess = runtimehost.PluginProcess

type providerProcess struct {
	*runtimehost.PluginProcess
	conn *grpc.ClientConn
}

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

func (c ExecConfig) processConfig() runtimehost.ProcessConfig {
	return runtimehost.ProcessConfig{
		Command:      c.Command,
		Args:         c.Args,
		Env:          c.Env,
		Egress:       cloneEgressPolicy(c.Egress),
		HostBinary:   c.HostBinary,
		Cleanup:      c.Cleanup,
		HostServices: c.HostServices,
		ProviderName: firstNonBlank(c.ProviderName, c.StaticSpec.Name),
		Telemetry:    c.Telemetry,
	}
}

func StartPluginProcess(ctx context.Context, cfg ProcessConfig) (*PluginProcess, error) {
	return runtimehost.StartPluginProcess(ctx, cfg)
}

func startProviderProcess(ctx context.Context, cfg ProcessConfig) (*providerProcess, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &providerProcess{
		PluginProcess: proc,
		conn:          proc.Conn(),
	}, nil
}

func NewPluginTempDir(pattern string) (string, error) {
	return runtimehost.NewPluginTempDir(pattern)
}

func cloneEgressPolicy(policy egress.Policy) egress.Policy {
	return egress.Policy{
		AllowedHosts:  append([]string(nil), policy.AllowedHosts...),
		DefaultAction: policy.DefaultAction,
	}
}

func ServeProvider(ctx context.Context, provider core.Provider) error {
	return pluginservice.ServeProvider(ctx, provider)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
