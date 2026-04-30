package plugins

import (
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

func providerServerGRPCOptions(providerName string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return grpcTelemetryOptions(metricutil.RPCRoleProviderServer, providerName, "", telemetry)
}

func grpcTelemetryOptions(role, providerName, hostService string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return metricutil.GRPCMetricOptions(telemetry, metricutil.RPCMetricDims{
		Role:            role,
		ProviderName:    providerName,
		HostServiceName: hostService,
	})
}
