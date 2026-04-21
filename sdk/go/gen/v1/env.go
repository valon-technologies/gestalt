package proto

const (
	// EnvProviderSocket is the Unix socket path an executable provider should bind.
	EnvProviderSocket = "GESTALT_PLUGIN_SOCKET"

	// EnvPluginInvokerSocket is the Unix socket path an executable provider can
	// use to invoke other declared plugin operations through the host.
	EnvPluginInvokerSocket = "GESTALT_PLUGIN_INVOKER_SOCKET"

	// EnvWorkflowManagerSocket is the Unix socket path an executable provider can
	// use to manage global workflow schedules through the host.
	EnvWorkflowManagerSocket = "GESTALT_WORKFLOW_MANAGER_SOCKET"

	// EnvProviderParentPID is the host process ID for executable providers that
	// want to terminate themselves when the parent goes away unexpectedly.
	EnvProviderParentPID = "GESTALT_PLUGIN_PARENT_PID"

	// CurrentProtocolVersion is the provider protocol version spoken by this
	// build of the host and SDK. Providers must echo this version in their
	// StartProviderResponse.
	CurrentProtocolVersion int32 = 2

	// Deprecated: Use EnvProviderSocket instead.
	EnvPluginSocket = EnvProviderSocket

	// Deprecated: Use EnvProviderParentPID instead.
	EnvPluginParentPID = EnvProviderParentPID
)
