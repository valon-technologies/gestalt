package coredata

import (
	"context"
	"errors"
	"fmt"

	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	"github.com/valon-technologies/gestalt/server/core"
)

func getStoredValue[T any](ctx context.Context, store idb.ObjectStore, key, operation string, decode func(idb.Record) *T) (*T, error) {
	rec, err := store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, idb.ErrNotFound) {
			return nil, core.ErrNotFound
		}
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return decode(rec), nil
}
