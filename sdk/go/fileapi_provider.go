package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// FileAPIProvider is implemented by providers that serve browser-style file
// APIs over gRPC.
type FileAPIProvider interface {
	Provider
	proto.FileAPIServer
}
