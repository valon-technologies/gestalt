package proto

const (
	// EnvProviderSocket is the Unix socket path an executable provider should bind.
	EnvProviderSocket = "GESTALT_PLUGIN_SOCKET"

	// EnvProviderParentPID is the host process ID for executable providers that
	// want to terminate themselves when the parent goes away unexpectedly.
	EnvProviderParentPID = "GESTALT_PLUGIN_PARENT_PID"

	// CurrentProtocolVersion is the provider protocol version spoken by this
	// build of the host and SDK. Providers must echo this version in their
	// StartProviderResponse.
	CurrentProtocolVersion int32 = 3

	// Deprecated: Use EnvProviderSocket instead.
	EnvPluginSocket = EnvProviderSocket

	// Deprecated: Use EnvProviderParentPID instead.
	EnvPluginParentPID = EnvProviderParentPID
)
