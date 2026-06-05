package operator

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

type SyncObservability struct {
	Recorder    *SyncMetricsRecorder
	BuildOutput providerpkg.CommandOutput
}

func (paths lifecyclePaths) stageOptions() providerpkg.StageSourcePreparedInstallOptions {
	return providerpkg.StageSourcePreparedInstallOptions{
		BuildOutput: paths.syncBuildOutput,
	}
}

func (paths lifecyclePaths) prefetchMaterializedCache(ctx context.Context, requests []materializedCacheRequest, parallelism int) {
	if paths.syncCache.remote == nil || len(requests) == 0 {
		return
	}
	stats := paths.syncCache.Prefetch(ctx, requests, parallelism)
	if paths.syncMetrics != nil {
		paths.syncMetrics.RecordCachePrefetch(stats)
	}
}

func (l *Lifecycle) prefetchAppMaterializedCache(paths lifecyclePaths, lock *Lockfile, work []*preparedAppWork, mode artifactMode, parallelism int) {
	if mode != artifactModeMaterialize || lock == nil || paths.syncCache.remote == nil {
		return
	}
	var requests []materializedCacheRequest
	for _, work := range work {
		if work == nil || !providerRequiresCommittedLock(work.entry) {
			continue
		}
		entry, ok := lock.Providers.App[work.name]
		if !ok || !needsLockedMaterializedCachePrefetch(paths, providermanifestv1.KindApp, work.name, work.name, work.entry, entry, providerDestDir(paths, work.name), mode) {
			continue
		}
		req, ok := l.materializedCacheRequestForLockedEntry(paths, providermanifestv1.KindApp, work.name, "provider "+strconv.Quote(work.name), entry, providerDestDir(paths, work.name))
		if ok {
			requests = append(requests, req)
		}
	}
	paths.prefetchMaterializedCache(context.Background(), requests, parallelism)
}

func (l *Lifecycle) prefetchComponentMaterializedCache(paths lifecyclePaths, lock *Lockfile, cfg *config.Config, mode artifactMode, parallelism int) {
	if mode != artifactModeMaterialize || lock == nil || cfg == nil || paths.syncCache.remote == nil {
		return
	}
	var requests []materializedCacheRequest
	for _, collection := range hostProviderCollections(cfg) {
		kind := providerManifestKind(collection.kind)
		requests = append(requests, l.materializedCacheRequestsForProviders(paths, lockEntriesForKind(lock, collection.kind), kind, collection.entries, func(name string) string {
			return componentDestDir(paths, collection.kind, name)
		}, func(name string) string {
			return name
		})...)
	}
	requests = append(requests, l.materializedCacheRequestsForProviders(paths, lockEntriesForProviderKind(lock, providermanifestv1.KindRuntime), providermanifestv1.KindRuntime, runtimeProviderEntries(cfg), func(name string) string {
		return runtimeDestDir(paths, name)
	}, func(name string) string {
		return name
	})...)
	requests = append(requests, l.materializedCacheRequestsForProviders(paths, lockEntriesForProviderKind(lock, providermanifestv1.KindIndexedDB), providermanifestv1.KindIndexedDB, cfg.Providers.IndexedDB, func(name string) string {
		return indexeddbDestDir(paths, name)
	}, func(name string) string {
		return name
	})...)
	requests = append(requests, l.materializedCacheRequestsForProviders(paths, lockEntriesForProviderKind(lock, providermanifestv1.KindS3), providermanifestv1.KindS3, cfg.Providers.S3, func(name string) string {
		return s3DestDir(paths, name)
	}, func(name string) string {
		return name
	})...)
	requests = append(requests, l.materializedCacheRequestsForProviders(paths, lockEntriesForProviderKind(lock, providermanifestv1.KindUI), providermanifestv1.KindUI, uiProviderEntries(cfg), func(name string) string {
		return uiDestDir(paths, name)
	}, func(name string) string {
		return "ui:" + name
	})...)
	paths.prefetchMaterializedCache(context.Background(), requests, parallelism)
}

func (l *Lifecycle) materializedCacheRequestsForProviders(paths lifecyclePaths, lockEntries map[string]LockEntry, kind string, entries map[string]*config.ProviderEntry, destDir func(string) string, fingerprintName func(string) string) []materializedCacheRequest {
	var requests []materializedCacheRequest
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[name]
		if !providerRequiresCommittedLock(entry) {
			continue
		}
		lockEntry, ok := lockEntries[name]
		if !ok {
			continue
		}
		dest := destDir(name)
		if !needsLockedMaterializedCachePrefetch(paths, kind, name, fingerprintName(name), entry, lockEntry, dest, artifactModeMaterialize) {
			continue
		}
		req, ok := l.materializedCacheRequestForLockedEntry(paths, kind, name, fmt.Sprintf("%s %q", kind, name), lockEntry, dest)
		if ok {
			requests = append(requests, req)
		}
	}
	return requests
}

func (l *Lifecycle) materializedCacheRequestForLockedEntry(paths lifecyclePaths, kind, name, subject string, entry LockEntry, destDir string) (materializedCacheRequest, bool) {
	platform := providerpkg.CurrentPlatformString()
	archiveLocation, resolvedKey, expectedSHA, err := l.resolveLockedArchiveDownload(paths, entry, platform, subject)
	if err != nil {
		return materializedCacheRequest{}, false
	}
	sourceKind := syncArtifactSourceLocalArchive
	if isRemoteReleaseMetadataLocation(archiveLocation) {
		sourceKind = syncArtifactSourceRemoteArchive
	}
	return materializedCacheRequest{
		Subject:        subject,
		Kind:           kind,
		Name:           name,
		SourceKind:     sourceKind,
		ArchiveSHA256:  expectedSHA,
		ResolvedKey:    resolvedKey,
		Platform:       platform,
		Package:        lockEntryPackage(entry),
		Version:        entry.Version,
		DestinationDir: destDir,
	}, true
}

func needsLockedMaterializedCachePrefetch(paths lifecyclePaths, kind, name, fingerprintName string, provider *config.ProviderEntry, entry LockEntry, destDir string, mode artifactMode) bool {
	if mode != artifactModeMaterialize || provider == nil || strings.TrimSpace(destDir) == "" {
		return false
	}
	if !lockEntrySourceMatchesProvider(paths, provider, entry) {
		return false
	}
	fingerprintMatches, err := lockEntryFingerprintMatchesProviderForMode(fingerprintName, provider, paths.configDir, entry, mode)
	if err != nil || !fingerprintMatches {
		return false
	}
	install, err := inspectPreparedInstall(destDir)
	if err != nil {
		return true
	}
	if !preparedInstallMatchesLockForMode(kind, name, provider, entry, install, mode) {
		return true
	}
	switch kind {
	case providermanifestv1.KindApp:
		return install.executablePath != "" && missingPathForPrefetch(install.executablePath)
	case providermanifestv1.KindUI:
		return install.assetRootPath == "" || missingPathForPrefetch(install.assetRootPath)
	default:
		return install.executablePath == "" || missingPathForPrefetch(install.executablePath)
	}
}

func missingPathForPrefetch(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}

func runtimeProviderEntries(cfg *config.Config) map[string]*config.ProviderEntry {
	entries := make(map[string]*config.ProviderEntry, len(cfg.Runtime.Providers))
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			entries[name] = &entry.ProviderEntry
		}
	}
	return entries
}

func uiProviderEntries(cfg *config.Config) map[string]*config.ProviderEntry {
	entries := make(map[string]*config.ProviderEntry, len(cfg.Providers.UI))
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			entries[name] = &entry.ProviderEntry
		}
	}
	return entries
}

type PreparedArtifactRoot struct {
	Subject string
	Kind    string
	Name    string
	DestDir string
}

func preparedArtifactRoots(paths lifecyclePaths, cfg *config.Config) []PreparedArtifactRoot {
	if cfg == nil {
		return nil
	}
	var roots []PreparedArtifactRoot
	for name, entry := range cfg.Apps {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: "provider " + strconv.Quote(name),
				Kind:    providermanifestv1.KindApp,
				Name:    name,
				DestDir: providerDestDir(paths, name),
			})
		}
	}
	for _, collection := range hostProviderCollections(cfg) {
		kind := providerManifestKind(collection.kind)
		for name, entry := range collection.entries {
			if entry != nil {
				roots = append(roots, PreparedArtifactRoot{
					Subject: fmt.Sprintf("%s %q", kind, name),
					Kind:    kind,
					Name:    name,
					DestDir: componentDestDir(paths, collection.kind, name),
				})
			}
		}
	}
	for name, entry := range cfg.Runtime.Providers {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: fmt.Sprintf("%s %q", providermanifestv1.KindRuntime, name),
				Kind:    providermanifestv1.KindRuntime,
				Name:    name,
				DestDir: runtimeDestDir(paths, name),
			})
		}
	}
	for name, entry := range cfg.Providers.IndexedDB {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: fmt.Sprintf("%s %q", providermanifestv1.KindIndexedDB, name),
				Kind:    providermanifestv1.KindIndexedDB,
				Name:    name,
				DestDir: indexeddbDestDir(paths, name),
			})
		}
	}
	for name, entry := range cfg.Providers.S3 {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: fmt.Sprintf("%s %q", providermanifestv1.KindS3, name),
				Kind:    providermanifestv1.KindS3,
				Name:    name,
				DestDir: s3DestDir(paths, name),
			})
		}
	}
	for name, entry := range cfg.Providers.UI {
		if entry != nil {
			roots = append(roots, PreparedArtifactRoot{
				Subject: "ui " + strconv.Quote(name),
				Kind:    providermanifestv1.KindUI,
				Name:    name,
				DestDir: uiDestDir(paths, name),
			})
		}
	}
	return dedupePreparedArtifactRoots(roots)
}
