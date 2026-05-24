package indexeddb

import (
	"context"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

// IndexedDB is the gestaltd database capability: SDK client surface plus server health checks.
type IndexedDB interface {
	idb.Database
	Pinger
}

// Pinger is implemented by server-side IndexedDB backends for readiness probes.
type Pinger interface {
	Ping(ctx context.Context) error
}
