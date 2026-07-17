// Package publicclient is the Go SDK for calling the public gestaltd API
// over REST or gRPC.
//
// External callers configure an address, transport, and auth explicitly:
//
//	client, err := publicclient.New(publicclient.Options{
//	    Address:   "https://valon.tools",
//	    Transport: publicclient.GRPC(),
//	    Auth:      publicclient.Bearer(func(ctx context.Context) (string, error) { ... }),
//	})
//
// Provider handlers derive a bound gRPC client from request context instead:
//
//	client, err := publicclient.GestaltFromContext(ctx)
package publicclient
