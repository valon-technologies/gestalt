package indexeddb

import (
	"context"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/remote"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
)

type gestaltRemoteIndexedDB struct {
	idb.Database
	client proto.IndexedDBClient
}

// NewGestaltRemoteProvider routes IndexedDB operations through a remote gestaltd public IndexedDB API.
func NewGestaltRemoteProvider(client proto.IndexedDBClient) coreindexeddb.IndexedDB {
	if client == nil {
		return nil
	}
	return &gestaltRemoteIndexedDB{
		Database: rpcidb.NewClient(client, rpcidb.Options{}),
		client:   client,
	}
}

func (db *gestaltRemoteIndexedDB) Ping(ctx context.Context) error {
	_, err := db.client.Count(ctx, &proto.ObjectStoreRangeRequest{Store: "__gestalt_ping__"})
	if err == nil {
		return nil
	}
	return remote.StatusError(err)
}

func (db *gestaltRemoteIndexedDB) Close() error { return nil }

var _ coreindexeddb.IndexedDB = (*gestaltRemoteIndexedDB)(nil)
