package bootstrap

import (
	"fmt"

	"github.com/valon-technologies/gestalt/server/services/providergateway"
	"google.golang.org/grpc"
)

// gatewayConn registers a raw gRPC connection as a direct endpoint on the
// provider gateway transport for the given provider target, then returns a
// gateway-routed connection that should be used to construct clients. If the
// transport is nil, the raw connection is returned unchanged so callers can
// safely use this in all build paths (e.g. tests without a gateway).
func gatewayConn(
	transport *providergateway.ProviderGatewayTransport,
	target providergateway.ProviderTarget,
	conn grpc.ClientConnInterface,
) (grpc.ClientConnInterface, error) {
	if transport == nil || conn == nil {
		return conn, nil
	}
	if err := transport.ReplaceDirect(target, providergateway.DirectEndpoint{Conn: conn}); err != nil {
		return nil, fmt.Errorf("register %s/%s provider gateway route: %w", target.Kind, target.Name, err)
	}
	return transport.Conn(target), nil
}
