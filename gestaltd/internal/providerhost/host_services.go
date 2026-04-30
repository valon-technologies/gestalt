package providerhost

import (
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type StartedHostService = runtimehost.StartedHostService
type StartedHostServices = runtimehost.StartedHostServices
type HostServicesOption = runtimehost.HostServicesOption

func WithHostServicesProviderName(name string) HostServicesOption {
	return runtimehost.WithHostServicesProviderName(name)
}

func WithHostServicesTelemetry(telemetry metricutil.TelemetryProviders) HostServicesOption {
	return runtimehost.WithHostServicesTelemetry(telemetry)
}

func StartHostServices(services []HostService, opts ...HostServicesOption) (*StartedHostServices, error) {
	return runtimehost.StartHostServices(services, opts...)
}
