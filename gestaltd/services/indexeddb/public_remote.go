package indexeddb

import (
	"context"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type indexedDBWithPing struct {
	idb.Database
}

func (indexedDBWithPing) Ping(context.Context) error { return nil }

// NewPublicRemote constructs a gestaltd-to-gestaltd IndexedDB client without runtime lifecycle.
func NewPublicRemote(client proto.IndexedDBClient) coreindexeddb.IndexedDB {
	return indexedDBWithPing{
		Database: rpcidb.NewClient(client, rpcidb.Options{
			UnaryTimeout: runtimehost.ProviderRPCTimeout,
		}),
	}
}

var _ coreindexeddb.IndexedDB = indexedDBWithPing{}
