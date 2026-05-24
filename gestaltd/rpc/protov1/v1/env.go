package proto

const (
	EnvProviderSocket      = "GESTALT_PROVIDER_SOCKET"
	EnvAppInvokerSocket    = "GESTALT_APP_INVOKER_SOCKET"
	EnvAgentHostSocket     = "GESTALT_AGENT_HOST_SOCKET"
	EnvAgentProviderSocket = "GESTALT_AGENT_PROVIDER_SOCKET"
	EnvProviderParentPID   = "GESTALT_APP_PARENT_PID"
	EnvProviderName        = "GESTALT_APP_NAME"
	EnvProviderTelemetry   = "GESTALT_PROVIDER_TELEMETRY"

	CurrentProtocolVersion int32 = 4
)
