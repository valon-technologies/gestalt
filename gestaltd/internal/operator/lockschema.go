package operator

import (
	"fmt"
	"maps"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

const (
	providerLockSchemaName         = "gestaltd-provider-lock"
	providerLockSchemaVersion      = 5
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
	Plugin              map[string]portableLockEntry `json:"plugin,omitempty"`
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
	InputDigest string                 `json:"inputDigest,omitempty"`
	Package     string                 `json:"package"`
	Kind        string                 `json:"kind"`
	Runtime     string                 `json:"runtime"`
	Source      string                 `json:"source,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Archives    map[string]LockArchive `json:"archives,omitempty"`
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
	return []string{
		providermanifestv1.KindPlugin,
		providermanifestv1.KindAuthentication,
		providermanifestv1.KindAuthorization,
		providermanifestv1.KindExternalCredentials,
		providermanifestv1.KindIndexedDB,
		providermanifestv1.KindCache,
		providermanifestv1.KindS3,
		providermanifestv1.KindWorkflow,
		providermanifestv1.KindAgent,
		providermanifestv1.KindRuntime,
		providermanifestv1.KindSecrets,
		providerLockKindTelemetry,
		providerLockKindAudit,
		providermanifestv1.KindUI,
	}
}

func lockEntriesForProviderKind(lock *Lockfile, kind string) map[string]LockEntry {
	if lock == nil {
		return nil
	}
	switch kind {
	case providermanifestv1.KindPlugin:
		return lock.Providers
	case providermanifestv1.KindAuthentication:
		return lock.Authentication
	case providermanifestv1.KindAuthorization:
		return lock.Authorization
	case providermanifestv1.KindExternalCredentials:
		return lock.ExternalCredentials
	case providermanifestv1.KindIndexedDB:
		return lock.IndexedDBs
	case providermanifestv1.KindCache:
		return lock.Caches
	case providermanifestv1.KindS3:
		return lock.S3
	case providermanifestv1.KindWorkflow:
		return lock.Workflows
	case providermanifestv1.KindAgent:
		return lock.Agents
	case providermanifestv1.KindRuntime:
		return lock.Runtimes
	case providermanifestv1.KindSecrets:
		return lock.Secrets
	case providerLockKindTelemetry:
		return lock.Telemetry
	case providerLockKindAudit:
		return lock.Audit
	case providermanifestv1.KindUI:
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
			Plugin:              portableEntriesFromLockEntries(lock.Providers, providermanifestv1.KindPlugin),
			Authentication:      portableEntriesFromLockEntries(lock.Authentication, providermanifestv1.KindAuthentication),
			Authorization:       portableEntriesFromLockEntries(lock.Authorization, providermanifestv1.KindAuthorization),
			ExternalCredentials: portableEntriesFromLockEntries(lock.ExternalCredentials, providermanifestv1.KindExternalCredentials),
			IndexedDB:           portableEntriesFromLockEntries(lock.IndexedDBs, providermanifestv1.KindIndexedDB),
			Cache:               portableEntriesFromLockEntries(lock.Caches, providermanifestv1.KindCache),
			S3:                  portableEntriesFromLockEntries(lock.S3, providermanifestv1.KindS3),
			Workflow:            portableEntriesFromLockEntries(lock.Workflows, providerLockKindWorkflow),
			Agent:               portableEntriesFromLockEntries(lock.Agents, providermanifestv1.KindAgent),
			Runtime:             portableEntriesFromLockEntries(lock.Runtimes, providermanifestv1.KindRuntime),
			Secrets:             portableEntriesFromLockEntries(lock.Secrets, providermanifestv1.KindSecrets),
			Telemetry:           portableEntriesFromLockEntries(lock.Telemetry, providerLockKindTelemetry),
			Audit:               portableEntriesFromLockEntries(lock.Audit, providerLockKindAudit),
			UI:                  portableEntriesFromLockEntries(lock.UIs, providermanifestv1.KindUI),
		},
	}
}

func (lock *providerLockfile) toLockfile() *Lockfile {
	runtimeLock := newLockfile()
	if lock == nil {
		return runtimeLock
	}
	runtimeLock.Providers = lockEntriesFromPortableEntries(lock.Providers.Plugin)
	runtimeLock.Authentication = lockEntriesFromPortableEntries(lock.Providers.Authentication)
	runtimeLock.Authorization = lockEntriesFromPortableEntries(lock.Providers.Authorization)
	runtimeLock.ExternalCredentials = lockEntriesFromPortableEntries(lock.Providers.ExternalCredentials)
	runtimeLock.IndexedDBs = lockEntriesFromPortableEntries(lock.Providers.IndexedDB)
	runtimeLock.Caches = lockEntriesFromPortableEntries(lock.Providers.Cache)
	runtimeLock.S3 = lockEntriesFromPortableEntries(lock.Providers.S3)
	runtimeLock.Workflows = lockEntriesFromPortableEntries(lock.Providers.Workflow)
	runtimeLock.Agents = lockEntriesFromPortableEntries(lock.Providers.Agent)
	runtimeLock.Runtimes = lockEntriesFromPortableEntries(lock.Providers.Runtime)
	runtimeLock.Secrets = lockEntriesFromPortableEntries(lock.Providers.Secrets)
	runtimeLock.Telemetry = lockEntriesFromPortableEntries(lock.Providers.Telemetry)
	runtimeLock.Audit = lockEntriesFromPortableEntries(lock.Providers.Audit)
	runtimeLock.UIs = lockEntriesFromPortableEntries(lock.Providers.UI)
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
			InputDigest: entry.Fingerprint,
			Package:     packageRef,
			Kind:        lockEntryKind(entry, kind),
			Runtime:     lockEntryRuntime(entry, kind),
			Source:      source,
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
		source := entry.Source
		if source == "" {
			source = entry.Package
		}
		runtimeEntries[name] = LockEntry{
			Fingerprint: entry.InputDigest,
			Package:     entry.Package,
			Kind:        providermanifestv1.NormalizeKind(entry.Kind),
			Runtime:     entry.Runtime,
			Source:      source,
			Version:     entry.Version,
			Archives:    maps.Clone(entry.Archives),
		}
	}
	return runtimeEntries
}
