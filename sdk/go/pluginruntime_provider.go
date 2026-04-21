package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// PluginRuntimeProvider is implemented by providers that manage hosted
// executable-plugin runtime sessions over gRPC.
type PluginRuntimeProvider interface {
	Provider
	proto.PluginRuntimeProviderServer
}
