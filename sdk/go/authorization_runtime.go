package gestalt

import (
	"context"
	"time"

	sdkauthorization "github.com/valon-technologies/gestalt/sdk/go/authorization"
	rpcauthorization "github.com/valon-technologies/gestalt/server/rpc/authorization"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var sharedAuthorizationTransport sharedManagerTransport[proto.AuthorizationProviderClient]

// Authorization returns a shared authorization capability.
func Authorization() (sdkauthorization.Runtime, error) {
	target, token, err := hostServiceTarget("authorization")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "authorization", target, token, &sharedAuthorizationTransport, proto.NewAuthorizationProviderClient)
	if err != nil {
		return nil, err
	}
	return rpcauthorization.NewClient(client, rpcauthorization.Options{}), nil
}
