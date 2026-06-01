package operator

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	providerLockSchemaName         = "gestaltd-provider-lock"
	providerLockSchemaVersion      = 8
	providerLockRevision           = 0
	providerLockKindWorkflow       = "workflow"
	providerLockKindTelemetry      = "telemetry"
	providerLockKindAudit          = "audit"
	providerLockRuntimeExecutable  = providerReleaseRuntimeExecutable
	providerLockRuntimeDeclarative = providerReleaseRuntimeDeclarative
	providerLockRuntimeUI          = providerReleaseRuntimeUI
	providerLockRuntimeAssets      = providerLockRuntimeUI
)

type providerLockfile struct {
	Schema        string              `json:"schema"`
	SchemaVersion int                 `json:"schemaVersion"`
	Revision      int                 `json:"revision"`
	Providers     providerLockBuckets `json:"providers"`
}

type providerLockBuckets struct {
	App                 map[string]portableLockEntry `json:"app,omitempty"`
	Authentication      map[string]portableLockEntry `json:"authentication,omitempty"`
	Authorization       map[string]portableLockEntry `json:"authorization,omitempty"`
	ExternalCredentials map[string]portableLockEntry `json:"externalCredentials,omitempty"`
	IndexedDB           map[string]portableLockEntry `json:"indexeddb,omitempty"`
	Cache               map[string]portableLockEntry `json:"cache,omitempty"`
	S3                  map[string]portableLockEntry `json:"s3,omitempty"`
	Workflow            map[string]portableLockEntry `json:"workflow,omitempty"`
	Agent               map[string]portableLockEntry `json:"agent,omitempty"`
	Runtime             map[string]portableLockEntry `json:"runtime,omitempty"`
	Secrets             map[string]portableLockEntry `json:"secrets,omitempty"`
	Telemetry           map[string]portableLockEntry `json:"telemetry,omitempty"`
	Audit               map[string]portableLockEntry `json:"audit,omitempty"`
	UI                  map[string]portableLockEntry `json:"ui,omitempty"`
}

type portableLockEntry struct {
	InputDigest        string                       `json:"inputDigest,omitempty"`
	Package            string                       `json:"package"`
	Kind               string                       `json:"kind"`
	Runtime            string                       `json:"runtime"`
	Source             string                       `json:"source,omitempty"`
	SourceRef          *LockSourceRef               `json:"sourceRef,omitempty"`
	Version            string                       `json:"version,omitempty"`
	Archives           map[string]LockArchive       `json:"archives,omitempty"`
	Manifest           *providermanifestv1.Manifest `json:"manifest,omitempty"`
	CatalogAvailable   bool                         `json:"catalogAvailable,omitempty"`
	CatalogFingerprint string                       `json:"catalogFingerprint,omitempty"`
	CatalogOperations  []string                     `json:"catalogOperations,omitempty"`
	CatalogSessionOnly bool                         `json:"catalogSessionOnly,omitempty"`
}

type providerLockBucketSpec struct {
	kind            string
	name            string
	runtimeEntries  func(*Lockfile) *map[string]LockEntry
	portableEntries func(*providerLockfile) *map[string]portableLockEntry
}

var providerLockBucketSpecs = []providerLockBucketSpec{
	newProviderLockBucket(providermanifestv1.KindApp, "app", func(lock *Lockfile) *map[string]LockEntry { return &lock.Providers }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.App }),
	newProviderLockBucket(providermanifestv1.KindAuthentication, "authentication", func(lock *Lockfile) *map[string]LockEntry { return &lock.Authentication }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Authentication }),
	newProviderLockBucket(providermanifestv1.KindAuthorization, "authorization", func(lock *Lockfile) *map[string]LockEntry { return &lock.Authorization }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Authorization }),
	newProviderLockBucket(providermanifestv1.KindExternalCredentials, "externalCredentials", func(lock *Lockfile) *map[string]LockEntry { return &lock.ExternalCredentials }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.ExternalCredentials }),
	newProviderLockBucket(providermanifestv1.KindIndexedDB, "indexeddb", func(lock *Lockfile) *map[string]LockEntry { return &lock.IndexedDBs }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.IndexedDB }),
	newProviderLockBucket(providermanifestv1.KindCache, "cache", func(lock *Lockfile) *map[string]LockEntry { return &lock.Caches }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Cache }),
	newProviderLockBucket(providermanifestv1.KindS3, "s3", func(lock *Lockfile) *map[string]LockEntry { return &lock.S3 }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.S3 }),
	newProviderLockBucket(providermanifestv1.KindWorkflow, "workflow", func(lock *Lockfile) *map[string]LockEntry { return &lock.Workflows }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Workflow }),
	newProviderLockBucket(providermanifestv1.KindAgent, "agent", func(lock *Lockfile) *map[string]LockEntry { return &lock.Agents }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Agent }),
	newProviderLockBucket(providermanifestv1.KindRuntime, "runtime", func(lock *Lockfile) *map[string]LockEntry { return &lock.Runtimes }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Runtime }),
	newProviderLockBucket(providermanifestv1.KindSecrets, "secrets", func(lock *Lockfile) *map[string]LockEntry { return &lock.Secrets }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Secrets }),
	newProviderLockBucket(providerLockKindTelemetry, "telemetry", func(lock *Lockfile) *map[string]LockEntry { return &lock.Telemetry }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Telemetry }),
	newProviderLockBucket(providerLockKindAudit, "audit", func(lock *Lockfile) *map[string]LockEntry { return &lock.Audit }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.Audit }),
	newProviderLockBucket(providermanifestv1.KindUI, "ui", func(lock *Lockfile) *map[string]LockEntry { return &lock.UIs }, func(lock *providerLockfile) *map[string]portableLockEntry { return &lock.Providers.UI }),
}

func newProviderLockBucket(kind, name string, runtimeEntries func(*Lockfile) *map[string]LockEntry, portableEntries func(*providerLockfile) *map[string]portableLockEntry) providerLockBucketSpec {
	return providerLockBucketSpec{
		kind:            kind,
		name:            name,
		runtimeEntries:  runtimeEntries,
		portableEntries: portableEntries,
	}
}

func (bucket providerLockBucketSpec) path() string {
	return "providers." + bucket.name
}

func (bucket providerLockBucketSpec) runtimeLockEntries(lock *Lockfile) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	return *bucket.runtimeEntries(lock)
}

func (bucket providerLockBucketSpec) setRuntimeLockEntries(lock *Lockfile, entries map[string]LockEntry) {
	*bucket.runtimeEntries(lock) = entries
}

func (bucket providerLockBucketSpec) portableLockEntries(lock *providerLockfile) map[string]portableLockEntry {
	if lock == nil {
		return nil
	}
	return *bucket.portableEntries(lock)
}

func (bucket providerLockBucketSpec) setPortableLockEntries(lock *providerLockfile, entries map[string]portableLockEntry) {
	*bucket.portableEntries(lock) = entries
}

func forEachPortableBucketPair(expected, committed *providerLockfile, fn func(path string, expectedEntries, committedEntries map[string]portableLockEntry)) {
	for _, bucket := range providerLockBucketSpecs {
		fn(bucket.path(), bucket.portableLockEntries(expected), bucket.portableLockEntries(committed))
	}
}

func newLockfile() *Lockfile {
	return &Lockfile{
		Providers:           make(map[string]LockEntry),
		Authentication:      make(map[string]LockEntry),
		Authorization:       make(map[string]LockEntry),
		ExternalCredentials: make(map[string]LockEntry),
		IndexedDBs:          make(map[string]LockEntry),
		Caches:              make(map[string]LockEntry),
		S3:                  make(map[string]LockEntry),
		Workflows:           make(map[string]LockEntry),
		Agents:              make(map[string]LockEntry),
		Runtimes:            make(map[string]LockEntry),
		Secrets:             make(map[string]LockEntry),
		Telemetry:           make(map[string]LockEntry),
		Audit:               make(map[string]LockEntry),
		UIs:                 make(map[string]LockEntry),
	}
}

func normalizeLockfile(lock *Lockfile) *Lockfile {
	if lock == nil {
		return newLockfile()
	}
	if lock.Providers == nil {
		lock.Providers = make(map[string]LockEntry)
	}
	if lock.Authentication == nil {
		lock.Authentication = make(map[string]LockEntry)
	}
	if lock.Authorization == nil {
		lock.Authorization = make(map[string]LockEntry)
	}
	if lock.ExternalCredentials == nil {
		lock.ExternalCredentials = make(map[string]LockEntry)
	}
	if lock.Secrets == nil {
		lock.Secrets = make(map[string]LockEntry)
	}
	if lock.Caches == nil {
		lock.Caches = make(map[string]LockEntry)
	}
	if lock.S3 == nil {
		lock.S3 = make(map[string]LockEntry)
	}
	if lock.Workflows == nil {
		lock.Workflows = make(map[string]LockEntry)
	}
	if lock.Agents == nil {
		lock.Agents = make(map[string]LockEntry)
	}
	if lock.Runtimes == nil {
		lock.Runtimes = make(map[string]LockEntry)
	}
	if lock.Telemetry == nil {
		lock.Telemetry = make(map[string]LockEntry)
	}
	if lock.Audit == nil {
		lock.Audit = make(map[string]LockEntry)
	}
	if lock.IndexedDBs == nil {
		lock.IndexedDBs = make(map[string]LockEntry)
	}
	if lock.UIs == nil {
		lock.UIs = make(map[string]LockEntry)
	}
	return lock
}

func providerLockKinds() []string {
	kinds := make([]string, 0, len(providerLockBucketSpecs))
	for _, bucket := range providerLockBucketSpecs {
		kinds = append(kinds, bucket.kind)
	}
	return kinds
}

func lockEntriesForProviderKind(lock *Lockfile, kind string) map[string]LockEntry {
	for _, bucket := range providerLockBucketSpecs {
		if bucket.kind == kind {
			return bucket.runtimeLockEntries(lock)
		}
	}
	return nil
}

func providerLockfileFromLockfile(lock *Lockfile) *providerLockfile {
	lock = normalizeLockfile(lock)
	portableLock := &providerLockfile{
		Schema:        providerLockSchemaName,
		SchemaVersion: providerLockSchemaVersion,
		Revision:      providerLockRevision,
	}
	for _, bucket := range providerLockBucketSpecs {
		bucket.setPortableLockEntries(portableLock, portableEntriesFromLockEntries(bucket.runtimeLockEntries(lock), bucket.kind))
	}
	return portableLock
}

func (lock *providerLockfile) toLockfile() *Lockfile {
	runtimeLock := newLockfile()
	if lock == nil {
		return runtimeLock
	}
	for _, bucket := range providerLockBucketSpecs {
		bucket.setRuntimeLockEntries(runtimeLock, lockEntriesFromPortableEntries(bucket.portableLockEntries(lock)))
	}
	return runtimeLock
}

func validateProviderLockfile(lock *providerLockfile) error {
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

func portableEntriesFromLockEntries(entries map[string]LockEntry, kind string) map[string]portableLockEntry {
	if len(entries) == 0 {
		return nil
	}
	portable := make(map[string]portableLockEntry, len(entries))
	for name := range entries {
		entry := entries[name]
		packageRef := lockEntryPackage(entry)
		source := strings.TrimSpace(entry.Source)
		if source == packageRef {
			source = ""
		}
		portable[name] = portableLockEntry{
			InputDigest:        entry.Fingerprint,
			Package:            packageRef,
			Kind:               lockEntryKind(entry, kind),
			Runtime:            lockEntryRuntime(entry, kind),
			Source:             source,
			SourceRef:          cloneLockSourceRef(entry.SourceRef),
			Version:            entry.Version,
			Archives:           maps.Clone(entry.Archives),
			Manifest:           entry.StaticManifest,
			CatalogAvailable:   entry.StaticCatalogAvailable,
			CatalogFingerprint: entry.StaticCatalogFingerprint,
			CatalogOperations:  slices.Clone(entry.StaticCatalogOperations),
			CatalogSessionOnly: entry.StaticCatalogSessionOnly,
		}
	}
	return portable
}

func lockEntriesFromPortableEntries(entries map[string]portableLockEntry) map[string]LockEntry {
	if len(entries) == 0 {
		return make(map[string]LockEntry)
	}
	runtimeEntries := make(map[string]LockEntry, len(entries))
	for name := range entries {
		entry := entries[name]
		source := entry.Source
		if source == "" {
			source = entry.Package
		}
		runtimeEntries[name] = LockEntry{
			Fingerprint:              entry.InputDigest,
			Package:                  entry.Package,
			Kind:                     providermanifestv1.NormalizeKind(entry.Kind),
			Runtime:                  entry.Runtime,
			Source:                   source,
			SourceRef:                cloneLockSourceRef(entry.SourceRef),
			Version:                  entry.Version,
			Archives:                 maps.Clone(entry.Archives),
			StaticManifest:           entry.Manifest,
			StaticCatalogAvailable:   entry.CatalogAvailable,
			StaticCatalogFingerprint: entry.CatalogFingerprint,
			StaticCatalogOperations:  slices.Clone(entry.CatalogOperations),
			StaticCatalogSessionOnly: entry.CatalogSessionOnly,
		}
	}
	return runtimeEntries
}

func cloneLockSourceRef(src *LockSourceRef) *LockSourceRef {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}
