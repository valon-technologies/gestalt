// Identity is the canonical alias client for the Authentication wire service.
// It is a handwritten alias over the generated Authentication client so the
// proto service, host binding, and wire protocol remain "authentication" for
// compatibility, while the public SDK surface exposes the canonical
// "identity" naming.

package client

import (
	"context"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	"google.golang.org/grpc"
)

// Identity is the canonical alias client for gestalt.provider.v1.Authentication.
// It connects to the same "authentication" host service as Authentication.
// Every transport error is converted to *GestaltError.
type Identity = Authentication

// NewIdentity creates an Identity client over an injected gRPC connection.
// It is the canonical alias for NewAuthentication.
func NewIdentity(conn grpc.ClientConnInterface) *Identity {
	return NewAuthentication(conn)
}

var connectIdentityConns host.ConnPool

// ConnectIdentity dials the "authentication" host service advertised through the
// GESTALT_HOST_SERVICE_SOCKET environment and returns a connected client.
// name selects a named binding; the empty string selects the default
// binding. Connections are pooled per binding and shared across clients for
// the life of the process. The first dial blocks until the connection is
// ready or ctx is done.
//
// It is the canonical alias for ConnectAuthentication.
func ConnectIdentity(ctx context.Context, name string) (*Identity, error) {
	target, token, err := host.Target("authentication")
	if err != nil {
		return nil, toGestaltError(err)
	}
	conn, err := connectIdentityConns.Conn(ctx, "authentication", target, token, name)
	if err != nil {
		return nil, toGestaltError(err)
	}
	return NewIdentity(conn), nil
}
