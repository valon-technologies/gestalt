package coredata

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func getStoredValues[T any](ctx context.Context, keys []string, get func(context.Context, string) (*T, error)) (map[string]*T, error) {
	out := make(map[string]*T, len(keys))
	for _, id := range keys {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		value, err := get(ctx, id)
		if errors.Is(err, core.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = value
	}
	return out, nil
}

func getRequiredStoredValue[T any](ctx context.Context, store idb.ObjectStore, key, operation, field string, decode func(idb.Record) *T) (*T, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("%s: %s is required", operation, field)
	}
	return getStoredValue(ctx, store, key, operation, decode)
}
