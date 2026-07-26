package appregistry

import (
	"context"
	"sync"
)

// RetentionCatalogStore mutates apps/{app}/retention.json with optimistic concurrency.
type RetentionCatalogStore interface {
	MutateRetention(ctx context.Context, registryName, appName string, mutate func(*RetentionIndex) (bool, error)) error
}
type MemoryRetentionCatalogStore struct {
	mu      sync.Mutex
	indices map[string]*RetentionIndex
}

func NewMemoryRetentionCatalogStore() *MemoryRetentionCatalogStore {
	return &MemoryRetentionCatalogStore{
		indices: map[string]*RetentionIndex{},
	}
}

func retentionCatalogKey(registryName, appName string) string {
	return registryName + "/" + appName
}

func (s *MemoryRetentionCatalogStore) MutateRetention(_ context.Context, registryName, appName string, mutate func(*RetentionIndex) (bool, error)) error {
	if s == nil {
		return nil
	}
	key := retentionCatalogKey(registryName, appName)
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.indices[key]
	if index == nil {
		index = NewEmptyRetentionIndex()
	} else {
		index = cloneRetentionIndex(index)
	}
	changed, err := mutate(index)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	s.indices[key] = index
	return nil
}

func (s *MemoryRetentionCatalogStore) Get(registryName, appName string) *RetentionIndex {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneRetentionIndex(s.indices[retentionCatalogKey(registryName, appName)])
}

func cloneRetentionIndex(index *RetentionIndex) *RetentionIndex {
	if index == nil {
		return NewEmptyRetentionIndex()
	}
	out := &RetentionIndex{
		SchemaVersion: index.SchemaVersion,
		Versions:      make(map[string]RetentionVersion, len(index.Versions)),
	}
	for version, entry := range index.Versions {
		out.Versions[version] = cloneRetentionVersion(entry)
	}
	return out
}

func cloneRetentionVersion(entry RetentionVersion) RetentionVersion {
	out := entry
	out.PublishedAt = entry.PublishedAt.UTC()
	out.LastDeactivatedAt = cloneTimePtr(entry.LastDeactivatedAt)
	out.DeployableUntil = cloneTimePtr(entry.DeployableUntil)
	out.FirstDeployedAt = cloneTimePtr(entry.FirstDeployedAt)
	out.LockedAt = cloneTimePtr(entry.LockedAt)
	return out
}
