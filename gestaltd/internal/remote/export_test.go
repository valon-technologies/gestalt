package remote

var (
	// RemoteGRPCTarget exposes dial target parsing for tests.
	RemoteGRPCTarget = remoteGRPCTarget
	// NewTestClientSet builds a ClientSet from an existing connection for tests.
	NewTestClientSet = clientSetFromConn
)
