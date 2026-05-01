package providerpkg

import (
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
)

func ArchiveDigest(archivePath string) (string, error) {
	return packageio.ArchiveDigest(archivePath)
}

func FileSHA256(path string) (string, error) {
	return packageio.FileSHA256(path)
}

func DirectoryDigest(dirPath, manifestPath string, manifest *providermanifestv1.Manifest) (string, error) {
	return packageio.DirectoryDigest(dirPath, manifestPath, manifest)
}
