package indexeddb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
)

// OpenOptions configures indexeddb.Open.
type OpenOptions struct {
	Binding string
}

var sharedTransports sync.Map

// Open connects to the IndexedDB provider exposed by gestaltd.
func Open(ctx context.Context, opts OpenOptions) (Database, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	target, token, err := host.Target("indexeddb")
	if err != nil {
		return nil, err
	}
	binding := strings.TrimSpace(opts.Binding)
	transport := getSharedTransport(binding)
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, err := host.ServiceClient(dialCtx, "indexeddb", target, token, binding, transport, proto.NewIndexedDBClient)
	if err != nil {
		return nil, fmt.Errorf("indexeddb: connect to host: %w", err)
	}
	return &HostClient{client: client}, nil
}

func getSharedTransport(binding string) *host.SharedTransport[proto.IndexedDBClient] {
	val, _ := sharedTransports.LoadOrStore(binding, &host.SharedTransport[proto.IndexedDBClient]{})
	return val.(*host.SharedTransport[proto.IndexedDBClient])
}
