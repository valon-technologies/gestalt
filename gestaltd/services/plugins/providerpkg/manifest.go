package providerpkg

import (
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
)

const ManifestFile = packageio.ManifestFile

var ManifestFiles = packageio.ManifestFiles

const (
	ManifestFormatJSON = packageio.ManifestFormatJSON
	ManifestFormatYAML = packageio.ManifestFormatYAML
)

func FindManifestFile(dir string) (string, error) {
	return packageio.FindManifestFileIn(dir, ManifestFiles)
}

func IsManifestFile(path string) bool {
	return packageio.IsManifestFileIn(path, ManifestFiles)
}

func ManifestFormatFromPath(path string) string {
	return packageio.ManifestFormatFromPath(path)
}

func DecodeManifest(data []byte) (*providermanifestv1.Manifest, error) {
	return packageio.DecodeManifest(data)
}

func DecodeSourceManifestFormat(data []byte, format string) (*providermanifestv1.Manifest, error) {
	return packageio.DecodeSourceManifestFormat(data, format)
}

func DecodeManifestFormat(data []byte, format string) (*providermanifestv1.Manifest, error) {
	return packageio.DecodeManifestFormat(data, format)
}

func ManifestKind(manifest *providermanifestv1.Manifest) (string, error) {
	return packageio.ManifestKind(manifest)
}

func ValidatePolicyBoundUIRoutes(routes []providermanifestv1.UIRoute) error {
	return packageio.ValidatePolicyBoundUIRoutes(routes)
}

func CurrentPlatformArtifact(manifest *providermanifestv1.Manifest) (*providermanifestv1.Artifact, error) {
	return packageio.CurrentPlatformArtifact(manifest)
}

func EntrypointForKind(manifest *providermanifestv1.Manifest, kind string) *providermanifestv1.Entrypoint {
	return packageio.EntrypointForKind(manifest, kind)
}

func EnsureEntrypoint(manifest *providermanifestv1.Manifest) *providermanifestv1.Entrypoint {
	return packageio.EnsureEntrypoint(manifest)
}

func EncodeManifest(manifest *providermanifestv1.Manifest) ([]byte, error) {
	return packageio.EncodeManifest(manifest)
}

func EncodeManifestFormat(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	return packageio.EncodeManifestFormat(manifest, format)
}

func EncodeSourceManifestFormat(manifest *providermanifestv1.Manifest, format string) ([]byte, error) {
	return packageio.EncodeSourceManifestFormat(manifest, format)
}

func ManifestEqual(a, b *providermanifestv1.Manifest) bool {
	return packageio.ManifestEqual(a, b)
}

func cloneManifest(manifest *providermanifestv1.Manifest) (*providermanifestv1.Manifest, error) {
	return packageio.CloneManifest(manifest)
}
