package providerpkg

import (
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
)

func ReadPackageManifest(packagePath string) ([]byte, *providermanifestv1.Manifest, error) {
	return packageio.ReadPackageManifestIn(packagePath, ManifestFiles)
}

func ReadManifestFile(p string) ([]byte, *providermanifestv1.Manifest, error) {
	return packageio.ReadManifestFile(p)
}

func ReadSourceManifestFile(p string) ([]byte, *providermanifestv1.Manifest, error) {
	return packageio.ReadSourceManifestFile(p)
}

func LoadManifestFromPath(inputPath string) ([]byte, *providermanifestv1.Manifest, string, error) {
	return packageio.LoadManifestFromPathIn(inputPath, ManifestFiles)
}

func CopyPackageDir(sourceDir, destDir string) error {
	return packageio.CopyPackageDir(sourceDir, destDir)
}

func CreatePackageFromDir(sourceDir, outputPath string) error {
	return packageio.CreatePackageFromDir(sourceDir, outputPath)
}

func ExtractPackage(packagePath, destDir string) error {
	return packageio.ExtractPackage(packagePath, destDir)
}

func ReadArchiveEntry(packagePath, wanted string) ([]byte, error) {
	return packageio.ReadArchiveEntry(packagePath, wanted)
}

func ValidatePackageDir(sourceDir string) (*providermanifestv1.Manifest, error) {
	return packageio.ValidatePackageDir(sourceDir)
}
