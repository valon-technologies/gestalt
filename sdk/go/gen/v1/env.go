package proto

const (
	// EnvPluginSocket is the Unix socket path an executable plugin should bind.
	EnvPluginSocket = "GESTALT_PLUGIN_SOCKET"

	// CurrentProtocolVersion is the plugin protocol version spoken by this
	// build of the host and SDK. Plugins must echo this version in their
	// StartProviderResponse.
	CurrentProtocolVersion int32 = 2
)
