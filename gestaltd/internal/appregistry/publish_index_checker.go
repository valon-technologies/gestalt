package appregistry

import (
	"context"
	"fmt"
	"strings"
)

// StoreIndexChecker reads publication state from a RegistryObjectStore-backed index.
type StoreIndexChecker struct {
	Store       WritableRegistryStore
	StorageRoot string
}

func (c StoreIndexChecker) VersionPublished(ctx context.Context, storageRoot, appName, version string) (bool, error) {
	_ = ctx
	if c.Store == nil {
		return false, nil
	}
	storageRoot = strings.TrimSpace(storageRoot)
	if storageRoot == "" {
		storageRoot = strings.TrimSpace(c.StorageRoot)
	}
	if storageRoot == "" {
		return false, fmt.Errorf("storage root is required")
	}
	indexURL := StorageURL(storageRoot, AppIndexPath(appName))
	_, data, err := c.Store.ReadObject(indexURL)
	if err != nil {
		return false, err
	}
	index, err := decodeIndexOrEmpty(data)
	if err != nil {
		return false, err
	}
	if index == nil || index.Apps == nil {
		return false, nil
	}
	appVersions, ok := index.Apps[appName]
	if !ok {
		return false, nil
	}
	_, ok = appVersions.Versions[strings.TrimSpace(version)]
	return ok, nil
}
