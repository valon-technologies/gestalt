package providerhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	indexeddbservice "github.com/valon-technologies/gestalt/server/services/indexeddb"
)

type IndexedDBExecConfig = indexeddbservice.ExecConfig
type IndexedDBServerOptions = indexeddbservice.ServerOptions

func NewExecutableIndexedDB(ctx context.Context, cfg IndexedDBExecConfig) (coreindexeddb.IndexedDB, error) {
	return indexeddbservice.NewExecutable(ctx, cfg)
}

func NewIndexedDBServer(ds coreindexeddb.IndexedDB, pluginName string, opts IndexedDBServerOptions) proto.IndexedDBServer {
	return indexeddbservice.NewServer(ds, pluginName, opts)
}
