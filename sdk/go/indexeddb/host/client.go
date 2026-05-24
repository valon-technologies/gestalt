package host

import (
	"time"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	proto "github.com/valon-technologies/gestalt/sdk/go/protov1/v1"
	"google.golang.org/grpc"
)

// New returns a Database implementation over an existing IndexedDB gRPC client.
func New(client proto.IndexedDBClient) idb.Database {
	return &HostClient{client: client}
}

// NewConn returns a Database over a gRPC connection using the SDK datastore client stub.
func NewConn(conn grpc.ClientConnInterface) idb.Database {
	return New(proto.NewIndexedDBClient(conn))
}

// NewProvider returns a Database that applies rpcTimeout to unary datastore RPCs.
// Streaming transactions and cursors use the caller context without an added deadline.
func NewProvider(client proto.IndexedDBClient, rpcTimeout time.Duration) idb.Database {
	return &HostClient{client: client, rpcConfig: rpcConfig{rpcTimeout: rpcTimeout}}
}

// NewProviderConn is like NewProvider but constructs the client stub from conn.
func NewProviderConn(conn grpc.ClientConnInterface, rpcTimeout time.Duration) idb.Database {
	return NewProvider(proto.NewIndexedDBClient(conn), rpcTimeout)
}
