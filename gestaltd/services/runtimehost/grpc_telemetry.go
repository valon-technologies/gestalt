package runtimehost

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

func providerClientGRPCOptions(providerName string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return grpcTelemetryOptions(metricutil.RPCRoleProviderClient, providerName, "", telemetry)
}

func hostServiceServerGRPCOptions(providerName string, service HostService, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return grpcTelemetryOptions(metricutil.RPCRoleHostServiceServer, providerName, hostServiceMetricName(service), telemetry)
}

func grpcTelemetryOptions(role, providerName, hostService string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return metricutil.GRPCMetricOptions(telemetry, metricutil.RPCMetricDims{
		Role:            role,
		ProviderName:    providerName,
		HostServiceName: hostService,
	})
}

func hostServiceMetricName(service HostService) string {
	return strings.TrimSpace(service.Name)
}
