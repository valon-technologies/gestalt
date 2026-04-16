package gestalt

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validateProtocolVersion(version int32) error {
	if version == proto.CurrentProtocolVersion {
		return nil
	}
	return status.Errorf(
		codes.FailedPrecondition,
		"host requested protocol version %d, provider requires %d",
		version,
		proto.CurrentProtocolVersion,
	)
}
