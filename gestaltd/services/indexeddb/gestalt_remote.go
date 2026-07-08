package indexeddb

import (
	"context"
	"fmt"
	"strings"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	coreindexeddb "github.com/valon-technologies/gestalt/server/core/indexeddb"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	rpcidb "github.com/valon-technologies/gestalt/server/rpc/indexeddb"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type gestaltRemoteIndexedDB struct {
	idb.Database
}

// NewGestaltRemoteProvider routes indexeddb operations through a remote gestaltd public IndexedDB API.
func NewGestaltRemoteProvider(name string, client proto.IndexedDBClient) (coreindexeddb.IndexedDB, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("indexeddb provider name is required")
	}
	if client == nil {
		return nil, fmt.Errorf("indexeddb provider client is required")
	}
	db := rpcidb.NewClient(client, rpcidb.Options{
		UnaryTimeout: runtimehost.ProviderRPCTimeout,
		Binding:      name,
	})
	return &gestaltRemoteIndexedDB{Database: db}, nil
}

func (r *gestaltRemoteIndexedDB) Ping(ctx context.Context) error {
	ctx, cancel := runtimehost.ProviderCallContext(ctx)
	defer cancel()
	store := r.Database.ObjectStore("__gestalt_remote_ping__")
	_, err := store.Get(ctx, "__gestalt_remote_ping__")
	return err
}

func (r *gestaltRemoteIndexedDB) Close() error { return nil }

var _ coreindexeddb.IndexedDB = (*gestaltRemoteIndexedDB)(nil)
