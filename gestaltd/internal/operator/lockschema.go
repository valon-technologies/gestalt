package operator

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	providerLockSchemaName         = "gestaltd-provider-lock"
	providerLockSchemaVersion      = 11
	providerLockRevision           = 0
	providerLockKindWorkflow       = "workflow"
	providerLockKindTelemetry      = "telemetry"
	providerLockKindAudit          = "audit"
	providerLockRuntimeExecutable  = providerrelease.RuntimeExecutable
	providerLockRuntimeDeclarative = providerrelease.RuntimeDeclarative
	providerLockRuntimeUI          = providerrelease.RuntimeUI
	providerLockRuntimeAssets      = providerLockRuntimeUI
)

type providerLockBuckets struct {
	App                 map[string]LockEntry `json:"app,omitempty"`
	Identity            map[string]LockEntry `json:"identity,omitempty"`
	Authorization       map[string]LockEntry `json:"authorization,omitempty"`
	ExternalCredentials map[string]LockEntry `json:"externalCredentials,omitempty"`
	IndexedDB           map[string]LockEntry `json:"indexeddb,omitempty"`
	Cache               map[string]LockEntry `json:"cache,omitempty"`
	S3                  map[string]LockEntry `json:"s3,omitempty"`
	Workflow            map[string]LockEntry `json:"workflow,omitempty"`
	Agent               map[string]LockEntry `json:"agent,omitempty"`
	Runtime             map[string]LockEntry `json:"runtime,omitempty"`
	Secrets             map[string]LockEntry `json:"secrets,omitempty"`
	Telemetry           map[string]LockEntry `json:"telemetry,omitempty"`
	Audit               map[string]LockEntry `json:"audit,omitempty"`
	UI                  map[string]LockEntry `json:"ui,omitempty"`
}

type providerLockBucketSpec struct {
	kind    string
	name    string
	entries func(*Lockfile) *map[string]LockEntry
}

var providerLockBucketSpecs = []providerLockBucketSpec{
	newProviderLockBucket(providermanifestv1.KindApp, "app", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.App }),
	newProviderLockBucket(providermanifestv1.KindIdentity, "identity", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Identity }),
	newProviderLockBucket(providermanifestv1.KindAuthorization, "authorization", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Authorization }),
	newProviderLockBucket(providermanifestv1.KindExternalCredentials, "externalCredentials", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.ExternalCredentials }),
	newProviderLockBucket(providermanifestv1.KindIndexedDB, "indexeddb", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.IndexedDB }),
	newProviderLockBucket(providermanifestv1.KindCache, "cache", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Cache }),
	newProviderLockBucket(providermanifestv1.KindS3, "s3", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.S3 }),
	newProviderLockBucket(providermanifestv1.KindWorkflow, "workflow", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Workflow }),
	newProviderLockBucket(providermanifestv1.KindAgent, "agent", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Agent }),
	newProviderLockBucket(providermanifestv1.KindRuntime, "runtime", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Runtime }),
	newProviderLockBucket(providermanifestv1.KindSecrets, "secrets", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Secrets }),
	newProviderLockBucket(providerLockKindTelemetry, "telemetry", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Telemetry }),
	newProviderLockBucket(providerLockKindAudit, "audit", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.Audit }),
	newProviderLockBucket(providermanifestv1.KindUI, "ui", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers.UI }),
}

func newProviderLockBucket(kind, name string, entries func(*Lockfile) *map[string]LockEntry) providerLockBucketSpec {
	return providerLockBucketSpec{
		kind:    kind,
		name:    name,
		entries: entries,
	}
}

func (bucket providerLockBucketSpec) path() string {
	return "providers." + bucket.name
}

func (bucket providerLockBucketSpec) runtimeLockEntries(lock *Lockfile) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	return *bucket.entries(lock)
}

func (bucket providerLockBucketSpec) setRuntimeLockEntries(lock *Lockfile, entries map[string]LockEntry) {
	*bucket.entries(lock) = entries
}

func forEachLockBucketPair(expected, committed *Lockfile, fn func(path string, expectedEntries, committedEntries map[string]LockEntry)) {
	for _, bucket := range providerLockBucketSpecs {
		fn(bucket.path(), bucket.runtimeLockEntries(expected), bucket.runtimeLockEntries(committed))
	}
}

func newLockfile() *Lockfile {
	return normalizeLockfile(&Lockfile{})
}

func normalizeLockfile(lock *Lockfile) *Lockfile {
	if lock == nil {
		return newLockfile()
	}
	lock.Schema = providerLockSchemaName
	lock.SchemaVersion = providerLockSchemaVersion
	lock.Revision = providerLockRevision
	for _, bucket := range providerLockBucketSpecs {
		if bucket.runtimeLockEntries(lock) == nil {
			bucket.setRuntimeLockEntries(lock, make(map[string]LockEntry))
			continue
		}
		bucket.setRuntimeLockEntries(lock, normalizeRuntimeLockEntries(bucket.runtimeLockEntries(lock)))
	}
	return lock
}

func lockEntriesForProviderKind(lock *Lockfile, kind string) map[string]LockEntry {
	for _, bucket := range providerLockBucketSpecs {
		if bucket.kind == kind {
			return bucket.runtimeLockEntries(lock)
		}
	}
	return nil
}

func canonicalLockfile(lock *Lockfile) *Lockfile {
	canonical := &Lockfile{
		Schema:        providerLockSchemaName,
		SchemaVersion: providerLockSchemaVersion,
		Revision:      providerLockRevision,
	}
	for _, bucket := range providerLockBucketSpecs {
		bucket.setRuntimeLockEntries(canonical, canonicalLockEntries(bucket.runtimeLockEntries(lock), bucket.kind))
	}
	return canonical
}

func validateProviderLockfile(lock *Lockfile) error {
	if lock == nil {
		return fmt.Errorf("unsupported lockfile schema; run `gestaltd lock` to upgrade")
	}
	if lock.Schema != providerLockSchemaName {
		return fmt.Errorf("unsupported lockfile schema %q; run `gestaltd lock` to upgrade", lock.Schema)
	}
	if lock.SchemaVersion != providerLockSchemaVersion {
		return fmt.Errorf("unsupported lockfile schema version %d; run `gestaltd lock` to upgrade", lock.SchemaVersion)
	}
	return nil
}

func canonicalLockEntries(entries map[string]LockEntry, kind string) map[string]LockEntry {
	if len(entries) == 0 {
		return nil
	}
	canonical := make(map[string]LockEntry, len(entries))
	for name := range entries {
		entry := entries[name]
		packageRef := lockEntryPackage(entry)
		source := strings.TrimSpace(entry.Source)
		if source == packageRef {
			source = ""
		}
		canonical[name] = LockEntry{
			InputDigest:        entry.InputDigest,
			Package:            packageRef,
			Kind:               lockEntryKind(entry, kind),
			Runtime:            lockEntryRuntime(entry, kind),
			Source:             source,
			SourceRef:          cloneLockSourceRef(entry.SourceRef),
			Version:            entry.Version,
			Archives:           normalizeLockArchives(entry.Archives),
			ValidationManifest: entry.ValidationManifest,
			CatalogAvailable:   entry.CatalogAvailable,
			CatalogFingerprint: entry.CatalogFingerprint,
			CatalogSessionOnly: entry.CatalogSessionOnly,
		}
	}
	return canonical
}

func normalizeRuntimeLockEntries(entries map[string]LockEntry) map[string]LockEntry {
	if len(entries) == 0 {
		return make(map[string]LockEntry)
	}
	runtimeEntries := make(map[string]LockEntry, len(entries))
	for name := range entries {
		entry := entries[name]
		if strings.TrimSpace(entry.Source) == "" {
			entry.Source = entry.Package
		}
		entry.Kind = providermanifestv1.NormalizeKind(entry.Kind)
		entry.SourceRef = cloneLockSourceRef(entry.SourceRef)
		entry.Archives = normalizeLockArchives(entry.Archives)
		runtimeEntries[name] = entry
	}
	return runtimeEntries
}

func normalizeLockArchives(archives map[string]LockArchive) map[string]LockArchive {
	if len(archives) == 0 {
		return nil
	}
	normalized := make(map[string]LockArchive, len(archives))
	for platform := range archives {
		archive := archives[platform]
		if sha, ok := canonicalArchiveSHA256(archive.SHA256); ok {
			archive.SHA256 = sha
		} else {
			archive.SHA256 = strings.TrimSpace(archive.SHA256)
		}
		normalized[platform] = archive
	}
	return normalized
}

func cloneLockSourceRef(src *LockSourceRef) *LockSourceRef {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}
