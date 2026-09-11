package coredata

import (
	"context"
	"strings"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

func getRecordsByIDs(ctx context.Context, store idb.ObjectStore, ids []string) ([]idb.Record, error) {
	queries := make([]any, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		queries = append(queries, id)
	}
	if len(queries) == 0 {
		return nil, nil
	}
	return store.GetAll(ctx, idb.AnyOf(queries[0], queries[1:]...))
}
