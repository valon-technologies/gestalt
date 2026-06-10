package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
)

type sharedManagerTransport[C any] = host.SharedTransport[C]

func managerTransportClient[C any](ctx context.Context, serviceName, target, token string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return host.ManagerClient(ctx, serviceName, target, token, transport, newClient)
}

func hostServiceTarget(serviceName string) (string, string, error) {
	return host.Target(serviceName)
}

func cloneRequestContext(reqCtx *proto.RequestContext) *proto.RequestContext {
	if reqCtx == nil {
		return nil
	}
	cloned, _ := gproto.Clone(reqCtx).(*proto.RequestContext)
	return cloned
}
