package providergateway

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type ProviderKind string

const (
	ProviderKindAuthorization ProviderKind = "authorization"
)

type GatewaySource string

const (
	GatewaySourceSDKGRPC  GatewaySource = "sdk_grpc"
	GatewaySourceInternal GatewaySource = "internal"
)

type TransportPath string

const (
	TransportPathDirect          TransportPath = "direct"
	TransportPathProviderGateway TransportPath = "provider_gateway"
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
	Source         GatewaySource
	CallerToken    string
	Payload        []byte
}

type ProviderGatewayResponse struct {
	Payload []byte
}
