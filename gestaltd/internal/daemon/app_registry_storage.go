package daemon

import "github.com/valon-technologies/gestalt/server/internal/appregistry"

func appRegistryObjectStore() *appregistry.GcloudObjectStore {
	return &appregistry.GcloudObjectStore{Runner: appRegistryCommandRunner{}}
}

func downloadAppRegistryObject(storageURL string) (int64, []byte, error) {
	return appRegistryObjectStore().ReadObject(storageURL)
}

func uploadAppRegistryIndexFile(localPath, storageURL, sourceRef string, generation int64) error {
	return appRegistryObjectStore().WriteCatalogObject(appregistry.WriteCatalogObjectInput{
		LocalPath:  localPath,
		StorageURL: storageURL,
		SourceRef:  sourceRef,
		Generation: generation,
	})
}

func writeTempJSON(pattern string, data []byte) (string, error) {
	return appregistry.WriteTempJSON(pattern, data)
}

func appPublishPreconditionFailed(err error) bool {
	return appregistry.CatalogPreconditionFailed(err)
}
