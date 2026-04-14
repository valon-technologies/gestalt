package gestalt

import proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"

// S3Provider is implemented by providers that serve an S3-compatible
// object-store surface over gRPC.
type S3Provider interface {
	Provider
	proto.S3Server
}
