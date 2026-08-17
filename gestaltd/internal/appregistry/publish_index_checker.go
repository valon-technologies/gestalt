package appregistry

import (
	"context"
	"strings"
)

// StoreIndexChecker reads publication state from a RegistryObjectStore-backed index.
type StoreIndexChecker struct {
	Store       WritableRegistryStore
	StorageRoot string
}

func (c StoreIndexChecker) VersionPublished(ctx context.Context, publicRoot, appName, version string) (bool, error) {
	_ = ctx
	if c.Store == nil {
		return false, nil
	}
	storageRoot := strings.TrimSpace(c.StorageRoot)
	if storageRoot == "" {
		storageRoot = publicRootToStorageRoot(publicRoot)
	}
	indexURL := StorageURL(storageRoot, AppIndexPath(appName))
	_, data, err := c.Store.ReadObject(indexURL)
	if err != nil || len(data) == 0 {
		return false, err
	}
	index, err := DecodeIndex(data)
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

func publicRootToStorageRoot(publicRoot string) string {
	publicRoot = strings.TrimSpace(publicRoot)
	if strings.HasPrefix(publicRoot, "gs://") {
		return publicRoot
	}
	const prefix = "https://storage.googleapis.com/"
	if strings.HasPrefix(publicRoot, prefix) {
		return "gs://" + strings.TrimPrefix(publicRoot, prefix)
	}
	return publicRoot
}
