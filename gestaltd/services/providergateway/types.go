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
	GatewaySourceHTTP     GatewaySource = "http"
	GatewaySourceSDKGRPC  GatewaySource = "sdk_grpc"
	GatewaySourceInternal GatewaySource = "internal"
)

type RequestContext = proto.RequestContext

type ProviderGateway interface {
	Invoke(ctx context.Context, req ProviderGatewayRequest) (ProviderGatewayResponse, error)
}

type ProviderGatewayRequest struct {
	ProviderID        string
	ProviderKind      ProviderKind
	FullMethod        string
	InvokingSubjectID string
	RequestContext    *RequestContext
	Source            GatewaySource
	Payload           []byte
}

type ProviderGatewayResponse struct {
	Payload []byte
}
