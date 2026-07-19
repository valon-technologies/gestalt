package providergateway

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
)

type ProviderKind string

const ProviderKindAuthorization ProviderKind = "authorization"

// ProviderTarget identifies one provider instance for routing.
type ProviderTarget struct {
	Kind ProviderKind
	Name string
}

// DirectEndpoint is a locally registered gRPC service implementation.
type DirectEndpoint struct {
	Desc   *grpc.ServiceDesc
	Server any
}

// Gateway exposes target-bound gRPC client connections for provider calls.
type Gateway interface {
	Conn(ProviderTarget) grpc.ClientConnInterface
}

type TransportPath string

const (
	TransportPathDirect          TransportPath = "direct"
	TransportPathProviderGateway TransportPath = "provider_gateway"
	TransportPathUnresolved      TransportPath = "unresolved"
)

type RequestContext = proto.RequestContext

type Transport interface {
	Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (ProviderGatewayResponse, error)
}

type AuthorizationProvider interface {
	CheckAccess(ctx context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error)
}

type Next func(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error)

type ProviderGatewayRequest struct {
	ProviderID     string
	ProviderKind   ProviderKind
	ServiceName    string
	Operation      string
	RequestContext *RequestContext
	Payload        []byte
}

type ProviderGatewayResponse struct {
	Payload []byte
}
