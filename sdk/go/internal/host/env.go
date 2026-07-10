package host

// Environment variables through which the daemon advertises its host
// service endpoint to provider processes.
const (
	EnvHostServiceSocket    = "GESTALT_HOST_SERVICE_SOCKET"
	EnvHostServiceToken     = "GESTALT_HOST_SERVICE_TOKEN"
	EnvHostServiceTLSCAFile = "GESTALT_HOST_SERVICE_TLS_CA_FILE"
	EnvHostServiceTLSCAPEM  = "GESTALT_HOST_SERVICE_TLS_CA_PEM"
	EnvHostServices         = "GESTALT_HOST_SERVICES"
	BindingMetadata         = "x-gestalt-host-binding"
	relayTokenHeader        = "x-gestalt-host-service-relay-token"
)
