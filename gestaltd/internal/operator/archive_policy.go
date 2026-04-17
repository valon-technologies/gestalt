package operator

import (
	"fmt"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func allowsGenericArchive(kind string, manifest *providermanifestv1.Manifest) bool {
	switch kind {
	case providermanifestv1.KindWebUI:
		return true
	case providermanifestv1.KindPlugin:
		return manifest != nil && manifest.IsDeclarativeOnlyProvider()
	default:
		return false
	}
}

func validateLockedArchivePolicy(subject, kind string, manifest *providermanifestv1.Manifest, entry LockEntry, platform, resolvedKey string) error {
	if allowsGenericArchive(kind, manifest) {
		return nil
	}
	if resolvedKey == platformKeyGeneric {
		return unsafeGenericArchiveError(subject, platform)
	}
	exact, exactOK := entry.Archives[platform]
	generic, genericOK := entry.Archives[platformKeyGeneric]
	if exactOK && genericOK && exact.URL != "" && exact.URL == generic.URL {
		return unsafeGenericArchiveError(subject, platform)
	}
	return nil
}

func unsafeGenericArchiveError(subject, platform string) error {
	return fmt.Errorf(
		"generic release archives are not allowed for %s on %s; publish an explicit %s archive or keep the package platform-neutral (webui or declarative/spec-only plugin package)",
		subject,
		platform,
		platform,
	)
}
