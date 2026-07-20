package providergateway

import (
	"google.golang.org/grpc"
)

type ProviderKind string

const (
	ProviderKindAuthorization ProviderKind = "authorization"
	ProviderKindApp           ProviderKind = "app"
	ProviderKindWorkflow      ProviderKind = "workflow"
	ProviderKindAgent         ProviderKind = "agent"
)

type ProviderTarget struct {
	Kind ProviderKind
	Name string
}

type DirectEndpoint struct {
	Conn grpc.ClientConnInterface
}

type Gateway interface {
	Conn(ProviderTarget) grpc.ClientConnInterface
}

type TransportPath string

const (
	TransportPathDirect     TransportPath = "direct"
	TransportPathUnresolved TransportPath = "unresolved"
)

type ProviderGatewayRequest struct {
	ProviderID   string
	ProviderKind ProviderKind
	ServiceName  string
	Operation    string
}
