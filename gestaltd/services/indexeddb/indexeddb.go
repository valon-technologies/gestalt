// Package indexeddb exposes IndexedDB provider transport primitives.
package indexeddb

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const DefaultSocketEnv = providerhost.DefaultIndexedDBSocketEnv

type ExecConfig = providerhost.IndexedDBExecConfig
type ServerOptions = providerhost.IndexedDBServerOptions

func SocketEnv(name string) string {
	return providerhost.IndexedDBSocketEnv(name)
}

func SocketTokenEnv(name string) string {
	return providerhost.IndexedDBSocketTokenEnv(name)
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (coreindexeddb.IndexedDB, error) {
	return providerhost.NewExecutableIndexedDB(ctx, cfg)
}

func NewServer(ds coreindexeddb.IndexedDB, pluginName string, opts ServerOptions) proto.IndexedDBServer {
	return providerhost.NewIndexedDBServer(ds, pluginName, opts)
}
