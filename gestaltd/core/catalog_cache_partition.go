package core

import (
	"context"
	"strings"
)

type CatalogCachePartition struct {
	Provider   string
	Connection string
	Instance   string
}

type catalogCachePartitionKey struct{}

func WithCatalogCachePartition(ctx context.Context, partition CatalogCachePartition) context.Context {
	partition.Provider = strings.TrimSpace(partition.Provider)
	partition.Connection = strings.TrimSpace(partition.Connection)
	partition.Instance = strings.TrimSpace(partition.Instance)
	return context.WithValue(ctx, catalogCachePartitionKey{}, partition)
}

func CatalogCachePartitionFromContext(ctx context.Context) CatalogCachePartition {
	partition, _ := ctx.Value(catalogCachePartitionKey{}).(CatalogCachePartition)
	return partition
}
