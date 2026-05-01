package providerpkg

import (
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/plugins/packageio"
)

type LocalPackageReference = packageio.LocalPackageReference

func LocalPackageReferences(manifest *providermanifestv1.Manifest) []LocalPackageReference {
	return packageio.LocalPackageReferences(manifest)
}

func ResolveManifestLocalReferences(manifest *providermanifestv1.Manifest, manifestPath string) *providermanifestv1.Manifest {
	return packageio.ResolveManifestLocalReferences(manifest, manifestPath)
}
