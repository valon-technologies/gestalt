package proto

const (
	EnvProviderSocket        = "GESTALT_PLUGIN_SOCKET"
	EnvPluginInvokerSocket   = "GESTALT_PLUGIN_INVOKER_SOCKET"
	EnvWorkflowManagerSocket = "GESTALT_WORKFLOW_MANAGER_SOCKET"
	EnvAgentHostSocket       = "GESTALT_AGENT_HOST_SOCKET"
	EnvAgentManagerSocket    = "GESTALT_AGENT_MANAGER_SOCKET"
	EnvProviderParentPID     = "GESTALT_PLUGIN_PARENT_PID"
	EnvProviderName          = "GESTALT_PLUGIN_NAME"
	EnvProviderTelemetry     = "GESTALT_PROVIDER_TELEMETRY"

	CurrentProtocolVersion int32 = 3

	// Deprecated: use EnvProviderSocket.
	EnvPluginSocket = EnvProviderSocket
	// Deprecated: use EnvProviderParentPID.
	EnvPluginParentPID = EnvProviderParentPID
)
