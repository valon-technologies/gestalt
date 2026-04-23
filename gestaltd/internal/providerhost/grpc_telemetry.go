package providerhost

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
)

func providerClientGRPCOptions(providerName string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return grpcTelemetryOptions("provider_client", providerName, "", telemetry)
}

func providerServerGRPCOptions(providerName string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return grpcTelemetryOptions("provider_server", providerName, "", telemetry)
}

func hostServiceServerGRPCOptions(providerName string, service HostService, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	return grpcTelemetryOptions("host_service_server", providerName, hostServiceMetricName(service), telemetry)
}

func grpcTelemetryOptions(role, providerName, hostService string, telemetry metricutil.TelemetryProviders) []otelgrpc.Option {
	attrs := []attribute.KeyValue{
		metricutil.AttrRPCRole.String(role),
	}
	if providerName != "" {
		attrs = append(attrs, metricutil.AttrProvider.String(metricutil.AttrValue(providerName)))
	}
	if hostService != "" {
		attrs = append(attrs, metricutil.AttrHostService.String(metricutil.AttrValue(hostService)))
	}
	return metricutil.GRPCOptions(telemetry, attrs...)
}

func hostServiceMetricName(service HostService) string {
	if service.Name != "" {
		return strings.TrimSpace(service.Name)
	}
	envVar := strings.TrimSpace(service.EnvVar)
	if envVar == "" {
		return ""
	}
	envVar = strings.TrimPrefix(envVar, "GESTALT_")
	envVar = strings.TrimSuffix(envVar, "_SOCKET")
	return strings.ToLower(envVar)
}
