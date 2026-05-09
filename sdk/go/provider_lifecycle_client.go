package gestalt

import (
	"context"
	"fmt"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ProbeProviderLifecycle checks that a provider lifecycle endpoint can answer
// an identity RPC. It is intended for runtime readiness loops that only need to
// know whether the provider server is accepting requests.
func ProbeProviderLifecycle(ctx context.Context, conn grpc.ClientConnInterface, opts ...grpc.CallOption) error {
	if conn == nil {
		return fmt.Errorf("provider lifecycle: connection is nil")
	}
	_, err := proto.NewProviderLifecycleClient(conn).GetProviderIdentity(ctx, &emptypb.Empty{}, opts...)
	return err
}
