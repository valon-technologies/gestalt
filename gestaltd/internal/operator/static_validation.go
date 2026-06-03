package operator

import "github.com/valon-technologies/gestalt/server/internal/config"

func lockEntryUsesPortableStaticManifest(entry *config.ProviderEntry, lockEntry LockEntry) bool {
	if entry == nil {
		return false
	}
	if entry.HasReleaseMetadataSource() {
		return true
	}
	if !entry.HasGitSource() || gitSourceMaterialization(gitSourceDef(entry)) != gitMaterializationSnapshot {
		return false
	}
	if len(lockEntry.Archives) == 0 || lockEntry.SourceRef == nil {
		return false
	}
	return lockEntry.SourceRef.Type == gitSourceRefType &&
		lockEntry.SourceRef.Materialization == gitMaterializationSnapshot &&
		gitSourceMatchesLockRef(entry, lockEntry.SourceRef)
}
