package gestalt

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"google.golang.org/grpc"
	gproto "google.golang.org/protobuf/proto"
)

type sharedManagerTransport[C any] = host.SharedTransport[C]
type protoMessage = gproto.Message

func managerTransportClient[C any](ctx context.Context, serviceName, target, token string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return host.ManagerClient(ctx, serviceName, target, token, transport, newClient)
}

func hostServiceTransportClient[C any](ctx context.Context, serviceName, target, token, binding string, transport *sharedManagerTransport[C], newClient func(grpc.ClientConnInterface) C) (C, error) {
	return host.ServiceClient(ctx, serviceName, target, token, binding, transport, newClient)
}

func dialHostService(ctx context.Context, serviceName, target, token, binding string) (*grpc.ClientConn, error) {
	return host.DialService(ctx, serviceName, target, token, binding)
}

func hostServiceTarget(serviceName string) (string, string, error) {
	return host.Target(serviceName)
}

func hostServiceDialOptions(token string, binding string) []grpc.DialOption {
	return host.DialOptions(token, binding)
}
