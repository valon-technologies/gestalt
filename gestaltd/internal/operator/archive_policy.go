package operator

import (
	"fmt"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func allowsGenericArchive(kind string, manifest *providermanifestv1.Manifest) bool {
	switch kind {
	case providermanifestv1.KindUI:
		return true
	case providermanifestv1.KindPlugin:
		return manifest != nil && manifest.IsDeclarativeOnlyProvider()
	default:
		return false
	}
}

func validateLockedArchivePolicy(subject, kind string, manifest *providermanifestv1.Manifest, entry LockEntry, platform, resolvedKey string) error {
	return validateLockedArchivePolicyWithGenericAllowance(subject, entry, platform, resolvedKey, allowsGenericArchive(kind, manifest))
}

func validateStaticLockedArchivePolicy(subject, kind string, entry LockEntry, platform, resolvedKey string) error {
	return validateLockedArchivePolicyWithGenericAllowance(subject, entry, platform, resolvedKey, allowsStaticGenericArchive(kind, entry))
}

func validateLockedArchivePolicyWithGenericAllowance(subject string, entry LockEntry, platform, resolvedKey string, allowsGeneric bool) error {
	if allowsGeneric {
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

func allowsStaticGenericArchive(kind string, entry LockEntry) bool {
	switch kind {
	case providermanifestv1.KindUI:
		return true
	case providermanifestv1.KindPlugin:
		return lockEntryRuntime(entry, kind) == providerReleaseRuntimeDeclarative
	default:
		return false
	}
}

func unsafeGenericArchiveError(subject, platform string) error {
	return fmt.Errorf(
		"generic release archives are not allowed for %s on %s; publish an explicit %s archive or keep the package platform-neutral (ui or declarative/spec-only plugin package)",
		subject,
		platform,
		platform,
	)
}
