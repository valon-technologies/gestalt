package externalcredentials

import (
	"context"
	"time"

	sdkexternalcredentials "github.com/valon-technologies/gestalt/sdk/go/externalcredentials"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type Options struct {
	UnaryTimeout time.Duration
}

// NewClient wraps a generated external-credential gRPC client as the SDK
// external-credentials contract.
func NewClient(grpcClient proto.ExternalCredentialProviderClient, opts Options) sdkexternalcredentials.ExternalCredentials {
	return &rpcClient{grpc: grpcClient, opts: opts}
}

// NewConn builds an external-credentials capability from a gRPC connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) sdkexternalcredentials.ExternalCredentials {
	return NewClient(proto.NewExternalCredentialProviderClient(conn), opts)
}

func attachTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		if ctx == nil {
			return context.Background(), func() {}
		}
		return ctx, func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *rpcClient) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return attachTimeout(ctx, c.opts.UnaryTimeout)
}
