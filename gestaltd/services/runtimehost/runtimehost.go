// Package runtimehost exposes the host-owned runtime process and host-service
// primitives used by executable and hosted provider runtimes.
package runtimehost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
	"google.golang.org/grpc"
)

// HostService is a host-owned gRPC service exposed to provider runtime
// processes through a socket and environment variable.
type HostService = providerhost.HostService

type StartedHostService = providerhost.StartedHostService
type StartedHostServices = providerhost.StartedHostServices
type HostServicesOption = providerhost.HostServicesOption
type TelemetryProviders = metricutil.TelemetryProviders

type ProcessConfig = providerhost.ProcessConfig
type PluginProcess = providerhost.PluginProcess

type RuntimeProviderMetadata = providerhost.RuntimeProviderMetadata

type AppendRuntimeLogEntry = runtimelogs.AppendEntry

type PublicHostService = providerhost.PublicHostService
type PublicHostServiceRegistry = providerhost.PublicHostServiceRegistry
type PublicHostServiceSessionVerifier = providerhost.PublicHostServiceSessionVerifier

const DefaultRuntimeLogHostSocketEnv = providerhost.DefaultRuntimeLogHostSocketEnv

func WithHostServicesProviderName(name string) HostServicesOption {
	return providerhost.WithHostServicesProviderName(name)
}

func WithHostServicesTelemetry(telemetry TelemetryProviders) HostServicesOption {
	return providerhost.WithHostServicesTelemetry(telemetry)
}

func StartHostServices(services []HostService, opts ...HostServicesOption) (*StartedHostServices, error) {
	return providerhost.StartHostServices(services, opts...)
}

func StartPluginProcess(ctx context.Context, cfg ProcessConfig) (*PluginProcess, error) {
	return providerhost.StartPluginProcess(ctx, cfg)
}

func NewPluginTempDir(pattern string) (string, error) {
	return providerhost.NewPluginTempDir(pattern)
}

func ConfigureRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient, expectedKind proto.ProviderKind, name string, config map[string]any) (*RuntimeProviderMetadata, error) {
	return providerhost.ConfigureRuntimeProvider(ctx, client, expectedKind, name, config)
}

func CheckRuntimeProviderHealth(ctx context.Context, client proto.ProviderLifecycleClient) error {
	return providerhost.CheckRuntimeProviderHealth(ctx, client)
}

func StartRuntimeProvider(ctx context.Context, client proto.ProviderLifecycleClient) error {
	return providerhost.StartRuntimeProvider(ctx, client)
}

func WithProviderMigrationTimeout(ctx context.Context) context.Context {
	return providerhost.WithProviderMigrationTimeout(ctx)
}

func NewPublicHostServiceRegistry() *PublicHostServiceRegistry {
	return providerhost.NewPublicHostServiceRegistry()
}

func NewRuntimeLogHostServer(runtimeProviderName string, appendLogs func(context.Context, string, string, []AppendRuntimeLogEntry) (int64, error)) proto.PluginRuntimeLogHostServer {
	return providerhost.NewRuntimeLogHostServer(runtimeProviderName, appendLogs)
}

func RegisterRuntimeLogHostServer(srv *grpc.Server, runtimeProviderName string, appendLogs func(context.Context, string, string, []AppendRuntimeLogEntry) (int64, error)) {
	proto.RegisterPluginRuntimeLogHostServer(srv, NewRuntimeLogHostServer(runtimeProviderName, appendLogs))
}
