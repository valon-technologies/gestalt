package proto

const (
	// EnvPluginSocket is the Unix socket path an executable plugin should bind.
	EnvPluginSocket = "GESTALT_PLUGIN_SOCKET"

	// EnvPluginParentPID is the host process ID for executable plugins that
	// want to terminate themselves when the parent goes away unexpectedly.
	EnvPluginParentPID = "GESTALT_PLUGIN_PARENT_PID"

	// EnvResourceHostSocket is the Unix socket path where the host exposes
	// resource capability services to a plugin that declared resource
	// dependencies.
	EnvResourceHostSocket = "GESTALT_RESOURCE_HOST_SOCKET"

	// CurrentProtocolVersion is the plugin protocol version spoken by this
	// build of the host and SDK. Plugins must echo this version in their
	// StartProviderResponse.
	CurrentProtocolVersion int32 = 2
)
