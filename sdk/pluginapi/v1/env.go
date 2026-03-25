package pluginapiv1

const (
	// EnvPluginSocket is the Unix socket path an executable plugin should bind.
	EnvPluginSocket = "GESTALT_PLUGIN_SOCKET"

	// EnvRuntimeHostSocket is the Unix socket path a runtime plugin should dial
	// to reach host callbacks such as capability listing and provider invocation.
	EnvRuntimeHostSocket = "GESTALT_RUNTIME_HOST_SOCKET"
)
