// Package publicclient is the Go SDK for calling the public gestaltd API
// over REST or gRPC.
//
// External callers configure an address, transport, and auth explicitly:
//
//	client, err := publicclient.NewREST(publicclient.AddressOptions{
//	    Address: "https://valon.tools",
//	    Auth:    publicclient.Bearer(func(ctx context.Context) (string, error) { ... }),
//	})
//
// Or for external gRPC:
//
//	client, err := publicclient.NewGRPC(publicclient.AddressOptions{
//	    Address: "https://valon.tools",
//	    Auth:    publicclient.Bearer(func(ctx context.Context) (string, error) { ... }),
//	})
//
// Provider handlers derive a bound gRPC client from request context instead:
//
//	client, err := publicclient.GestaltFromContext(ctx)
package publicclient
