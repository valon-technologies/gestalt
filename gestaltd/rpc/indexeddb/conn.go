package indexeddb

import (
	"context"
	"strings"
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const hostServiceBindingHeader = "x-gestalt-host-binding"

// Options configures an IndexedDB gRPC client.
type Options struct {
	// UnaryTimeout applies an additional deadline to each unary RPC when positive.
	UnaryTimeout time.Duration
	// Binding selects the remote IndexedDB host-service provider.
	Binding string
}

// NewClient returns a Database implementation backed by an existing gRPC stub.
func NewClient(c proto.IndexedDBClient, opts Options) idb.Database {
	return &clientDB{client: c, opts: opts}
}

// NewConn dials no transport; it builds a stub from an existing connection.
func NewConn(conn grpc.ClientConnInterface, opts Options) idb.Database {
	return NewClient(proto.NewIndexedDBClient(conn), opts)
}

func attachTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		if ctx == nil {
			return context.Background(), func() {}
		}
		return ctx, func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, timeout)
}

func callCtxWithOpts(ctx context.Context, opts Options) (context.Context, context.CancelFunc) {
	if binding := strings.TrimSpace(opts.Binding); binding != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, hostServiceBindingHeader, binding)
	}
	return attachTimeout(ctx, opts.UnaryTimeout)
}

func (db *clientDB) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return callCtxWithOpts(ctx, db.opts)
}
