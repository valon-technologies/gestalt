package operator

import (
	"fmt"
	"maps"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	providerLockSchemaName         = "gestaltd-provider-lock"
	providerLockSchemaVersion      = 2
	providerLockRevision           = 0
	providerLockKindTelemetry      = "telemetry"
	providerLockKindAudit          = "audit"
	providerLockRuntimeExecutable  = providerReleaseRuntimeExecutable
	providerLockRuntimeDeclarative = providerReleaseRuntimeDeclarative
	providerLockRuntimeWebUI       = providerReleaseRuntimeWebUI
	providerLockRuntimeAssets      = providerLockRuntimeWebUI
)

type providerLockfile struct {
	Schema        string              `json:"schema"`
	SchemaVersion int                 `json:"schemaVersion"`
	Revision      int                 `json:"revision"`
	Providers     providerLockBuckets `json:"providers"`
}

type providerLockBuckets struct {
	Plugin    map[string]portableLockEntry `json:"plugin,omitempty"`
	Auth      map[string]portableLockEntry `json:"auth,omitempty"`
	IndexedDB map[string]portableLockEntry `json:"indexeddb,omitempty"`
	Cache     map[string]portableLockEntry `json:"cache,omitempty"`
	S3        map[string]portableLockEntry `json:"s3,omitempty"`
	Secrets   map[string]portableLockEntry `json:"secrets,omitempty"`
	Telemetry map[string]portableLockEntry `json:"telemetry,omitempty"`
	Audit     map[string]portableLockEntry `json:"audit,omitempty"`
	WebUI     map[string]portableLockEntry `json:"webui,omitempty"`
}

type portableLockEntry struct {
	InputDigest string                 `json:"inputDigest,omitempty"`
	Fingerprint string                 `json:"fingerprint,omitempty"`
	Package     string                 `json:"package"`
	Kind        string                 `json:"kind"`
	Runtime     string                 `json:"runtime"`
	Source      string                 `json:"source,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Archives    map[string]LockArchive `json:"archives,omitempty"`
}

func newLockfile() *Lockfile {
	return &Lockfile{
		Version:    LockVersion,
		Providers:  make(map[string]LockProviderEntry),
		Auth:       make(map[string]LockEntry),
		IndexedDBs: make(map[string]LockEntry),
		Caches:     make(map[string]LockEntry),
		S3:         make(map[string]LockEntry),
		Secrets:    make(map[string]LockEntry),
		Telemetry:  make(map[string]LockEntry),
		Audit:      make(map[string]LockEntry),
		UIs:        make(map[string]LockUIEntry),
	}
}

func normalizeLockfile(lock *Lockfile) *Lockfile {
	if lock == nil {
		return newLockfile()
	}
	if lock.Version == 0 {
		lock.Version = LockVersion
	}
	if lock.Providers == nil {
		lock.Providers = make(map[string]LockProviderEntry)
	}
	if lock.Auth == nil {
		lock.Auth = make(map[string]LockEntry)
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
		lock.UIs = make(map[string]LockUIEntry)
	}
	return lock
}

func providerLockKinds() []string {
	return []string{
		providermanifestv1.KindPlugin,
		providermanifestv1.KindAuth,
		providermanifestv1.KindIndexedDB,
		providermanifestv1.KindCache,
		providermanifestv1.KindS3,
		providermanifestv1.KindSecrets,
		providerLockKindTelemetry,
		providerLockKindAudit,
		providermanifestv1.KindWebUI,
	}
}

func lockEntriesForProviderKind(lock *Lockfile, kind string) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	switch kind {
	case providermanifestv1.KindPlugin:
		return lock.Providers
	case providermanifestv1.KindAuth:
		return lock.Auth
	case providermanifestv1.KindIndexedDB:
		return lock.IndexedDBs
	case providermanifestv1.KindCache:
		return lock.Caches
	case providermanifestv1.KindS3:
		return lock.S3
	case providermanifestv1.KindSecrets:
		return lock.Secrets
	case providerLockKindTelemetry:
		return lock.Telemetry
	case providerLockKindAudit:
		return lock.Audit
	case providermanifestv1.KindWebUI:
		return lock.UIs
	default:
		return nil
	}
}

func providerLockfileFromLockfile(lock *Lockfile) *providerLockfile {
	lock = normalizeLockfile(lock)
	return &providerLockfile{
		Schema:        providerLockSchemaName,
		SchemaVersion: providerLockSchemaVersion,
		Revision:      providerLockRevision,
		Providers: providerLockBuckets{
			Plugin:    portableEntriesFromLockEntries(lock.Providers, providermanifestv1.KindPlugin),
			Auth:      portableEntriesFromLockEntries(lock.Auth, providermanifestv1.KindAuth),
			IndexedDB: portableEntriesFromLockEntries(lock.IndexedDBs, providermanifestv1.KindIndexedDB),
			Cache:     portableEntriesFromLockEntries(lock.Caches, providermanifestv1.KindCache),
			S3:        portableEntriesFromLockEntries(lock.S3, providermanifestv1.KindS3),
			Secrets:   portableEntriesFromLockEntries(lock.Secrets, providermanifestv1.KindSecrets),
			Telemetry: portableEntriesFromLockEntries(lock.Telemetry, providerLockKindTelemetry),
			Audit:     portableEntriesFromLockEntries(lock.Audit, providerLockKindAudit),
			WebUI:     portableEntriesFromLockEntries(lock.UIs, providermanifestv1.KindWebUI),
		},
	}
}

func (lock *providerLockfile) toLockfile() *Lockfile {
	runtimeLock := newLockfile()
	if lock == nil {
		return runtimeLock
	}
	runtimeLock.Providers = lockEntriesFromPortableEntries(lock.Providers.Plugin)
	runtimeLock.Auth = lockEntriesFromPortableEntries(lock.Providers.Auth)
	runtimeLock.IndexedDBs = lockEntriesFromPortableEntries(lock.Providers.IndexedDB)
	runtimeLock.Caches = lockEntriesFromPortableEntries(lock.Providers.Cache)
	runtimeLock.S3 = lockEntriesFromPortableEntries(lock.Providers.S3)
	runtimeLock.Secrets = lockEntriesFromPortableEntries(lock.Providers.Secrets)
	runtimeLock.Telemetry = lockEntriesFromPortableEntries(lock.Providers.Telemetry)
	runtimeLock.Audit = lockEntriesFromPortableEntries(lock.Providers.Audit)
	runtimeLock.UIs = lockEntriesFromPortableEntries(lock.Providers.WebUI)
	return runtimeLock
}

func validateProviderLockfile(lock *providerLockfile) error {
	if lock == nil {
		return fmt.Errorf("unsupported lockfile schema; run `gestaltd init` to upgrade")
	}
	if lock.Schema != providerLockSchemaName {
		return fmt.Errorf("unsupported lockfile schema %q; run `gestaltd init` to upgrade", lock.Schema)
	}
	switch lock.SchemaVersion {
	case 1, providerLockSchemaVersion:
	default:
		return fmt.Errorf("unsupported lockfile schema version %d; run `gestaltd init` to upgrade", lock.SchemaVersion)
	}
	return nil
}

func portableEntriesFromLockEntries(entries map[string]LockEntry, kind string) map[string]portableLockEntry {
	if len(entries) == 0 {
		return nil
	}
	portable := make(map[string]portableLockEntry, len(entries))
	for name, entry := range entries {
		portable[name] = portableLockEntry{
			InputDigest: entry.Fingerprint,
			Package:     lockEntryPackage(entry),
			Kind:        lockEntryKind(entry, kind),
			Runtime:     lockEntryRuntime(entry, kind),
			Source:      entry.Source,
			Version:     entry.Version,
			Archives:    maps.Clone(entry.Archives),
		}
	}
	return portable
}

func lockEntriesFromPortableEntries(entries map[string]portableLockEntry) map[string]LockEntry {
	if len(entries) == 0 {
		return make(map[string]LockEntry)
	}
	runtimeEntries := make(map[string]LockEntry, len(entries))
	for name, entry := range entries {
		fingerprint := entry.InputDigest
		if fingerprint == "" {
			fingerprint = entry.Fingerprint
		}
		runtimeEntries[name] = LockEntry{
			Fingerprint: fingerprint,
			Package:     entry.Package,
			Kind:        entry.Kind,
			Runtime:     entry.Runtime,
			Source:      entry.Source,
			Version:     entry.Version,
			Archives:    maps.Clone(entry.Archives),
		}
	}
	return runtimeEntries
}
