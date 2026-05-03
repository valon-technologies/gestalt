package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/operator"
	"github.com/valon-technologies/gestalt/server/internal/providerregistry"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

type providerLifecycleKind struct {
	kind         string
	manifestKind string
	collection   func(*yaml.Node) *yaml.Node
	entryMap     func(*config.Config) map[string]*config.ProviderEntry
	lockEntries  func(*operator.Lockfile) map[string]operator.LockEntry
}

type providerLifecycleRow struct {
	Kind          string
	Name          string
	Entry         *config.ProviderEntry
	Source        string
	Package       string
	Version       string
	LockedVersion string
	Status        string
}

type providerMutationResult struct {
	ConfigPath   string
	LockfilePath string
	LockWritten  bool
}

var providerLifecycleKinds = []providerLifecycleKind{
	{
		kind:         providermanifestv1.KindPlugin,
		manifestKind: providermanifestv1.KindPlugin,
		collection:   func(doc *yaml.Node) *yaml.Node { return mappingValueNodeLocal(doc, "plugins") },
		entryMap: func(cfg *config.Config) map[string]*config.ProviderEntry {
			if cfg == nil {
				return nil
			}
			return cfg.Plugins
		},
		lockEntries: func(lock *operator.Lockfile) map[string]operator.LockEntry {
			if lock == nil {
				return nil
			}
			return lock.Providers
		},
	},
	lifecycleHostProviderKind(providermanifestv1.KindAuthentication, providermanifestv1.KindAuthentication),
	lifecycleHostProviderKind(providermanifestv1.KindAuthorization, providermanifestv1.KindAuthorization),
	lifecycleHostProviderKind(providermanifestv1.KindExternalCredentials, providermanifestv1.KindExternalCredentials),
	lifecycleHostProviderKind(providermanifestv1.KindSecrets, providermanifestv1.KindSecrets),
	lifecycleHostProviderKind(string(config.HostProviderKindTelemetry), providermanifestv1.KindPlugin),
	lifecycleHostProviderKind(string(config.HostProviderKindAudit), providermanifestv1.KindPlugin),
	lifecycleHostProviderKind(providermanifestv1.KindIndexedDB, providermanifestv1.KindIndexedDB),
	lifecycleHostProviderKind(providermanifestv1.KindCache, providermanifestv1.KindCache),
	lifecycleHostProviderKind(providermanifestv1.KindS3, providermanifestv1.KindS3),
	lifecycleHostProviderKind(providermanifestv1.KindWorkflow, providermanifestv1.KindWorkflow),
	lifecycleHostProviderKind(providermanifestv1.KindAgent, providermanifestv1.KindAgent),
	{
		kind:         providermanifestv1.KindRuntime,
		manifestKind: providermanifestv1.KindRuntime,
		collection: func(doc *yaml.Node) *yaml.Node {
			return mappingValueNodeLocal(mappingValueNodeLocal(doc, "runtime"), "providers")
		},
		entryMap: func(cfg *config.Config) map[string]*config.ProviderEntry {
			if cfg == nil || cfg.Runtime.Providers == nil {
				return nil
			}
			entries := make(map[string]*config.ProviderEntry, len(cfg.Runtime.Providers))
			for name, entry := range cfg.Runtime.Providers {
				if entry != nil {
					entries[name] = &entry.ProviderEntry
				}
			}
			return entries
		},
		lockEntries: func(lock *operator.Lockfile) map[string]operator.LockEntry {
			if lock == nil {
				return nil
			}
			return lock.Runtimes
		},
	},
	{
		kind:         providermanifestv1.KindUI,
		manifestKind: providermanifestv1.KindUI,
		collection: func(doc *yaml.Node) *yaml.Node {
			return mappingValueNodeLocal(mappingValueNodeLocal(doc, "providers"), "ui")
		},
		entryMap: func(cfg *config.Config) map[string]*config.ProviderEntry {
			if cfg == nil || cfg.Providers.UI == nil {
				return nil
			}
			entries := make(map[string]*config.ProviderEntry, len(cfg.Providers.UI))
			for name, entry := range cfg.Providers.UI {
				if entry != nil {
					entries[name] = &entry.ProviderEntry
				}
			}
			return entries
		},
		lockEntries: func(lock *operator.Lockfile) map[string]operator.LockEntry {
			if lock == nil {
				return nil
			}
			return lock.UIs
		},
	},
}

func lifecycleHostProviderKind(kind, manifestKind string) providerLifecycleKind {
	return providerLifecycleKind{
		kind:         kind,
		manifestKind: manifestKind,
		collection: func(doc *yaml.Node) *yaml.Node {
			return mappingValueNodeLocal(mappingValueNodeLocal(doc, "providers"), configKindKey(kind))
		},
		entryMap: func(cfg *config.Config) map[string]*config.ProviderEntry {
			if cfg == nil {
				return nil
			}
			switch kind {
			case providermanifestv1.KindAuthentication:
				return cfg.Providers.Authentication
			case providermanifestv1.KindAuthorization:
				return cfg.Providers.Authorization
			case providermanifestv1.KindExternalCredentials:
				return cfg.Providers.ExternalCredentials
			case providermanifestv1.KindSecrets:
				return cfg.Providers.Secrets
			case string(config.HostProviderKindTelemetry):
				return cfg.Providers.Telemetry
			case string(config.HostProviderKindAudit):
				return cfg.Providers.Audit
			case providermanifestv1.KindIndexedDB:
				return cfg.Providers.IndexedDB
			case providermanifestv1.KindCache:
				return cfg.Providers.Cache
			case providermanifestv1.KindS3:
				return cfg.Providers.S3
			case providermanifestv1.KindWorkflow:
				return cfg.Providers.Workflow
			case providermanifestv1.KindAgent:
				return cfg.Providers.Agent
			default:
				return nil
			}
		},
		lockEntries: func(lock *operator.Lockfile) map[string]operator.LockEntry {
			if lock == nil {
				return nil
			}
			switch kind {
			case providermanifestv1.KindAuthentication:
				return lock.Authentication
			case providermanifestv1.KindAuthorization:
				return lock.Authorization
			case providermanifestv1.KindExternalCredentials:
				return lock.ExternalCredentials
			case providermanifestv1.KindSecrets:
				return lock.Secrets
			case string(config.HostProviderKindTelemetry):
				return lock.Telemetry
			case string(config.HostProviderKindAudit):
				return lock.Audit
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
			default:
				return nil
			}
		},
	}
}

func normalizeProviderLifecycleKind(kind string) string {
	kind = providermanifestv1.NormalizeKind(kind)
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case string(config.HostProviderKindTelemetry):
		return string(config.HostProviderKindTelemetry)
	case string(config.HostProviderKindAudit):
		return string(config.HostProviderKindAudit)
	default:
		return kind
	}
}

func providerLifecycleKindFor(kind string) (providerLifecycleKind, bool) {
	kind = normalizeProviderLifecycleKind(kind)
	for _, candidate := range providerLifecycleKinds {
		if candidate.kind == kind {
			return candidate, true
		}
	}
	return providerLifecycleKind{}, false
}

func requireProviderLifecycleKind(kind string) (providerLifecycleKind, error) {
	if entryKind, ok := providerLifecycleKindFor(kind); ok {
		return entryKind, nil
	}
	return providerLifecycleKind{}, fmt.Errorf("unknown provider kind %q", kind)
}

func printProviderRows(rows []providerLifecycleRow) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "KIND\tNAME\tSOURCE\tPACKAGE\tVERSION\tLOCKED\tSTATUS")
	for _, row := range rows {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Kind,
			row.Name,
			row.Source,
			row.Package,
			row.Version,
			row.LockedVersion,
			row.Status,
		)
	}
	_ = w.Flush()
}

func providerLifecycleRows(cfg *config.Config, configPath string, lock *operator.Lockfile, kindFilter string) ([]providerLifecycleRow, error) {
	if strings.TrimSpace(kindFilter) != "" {
		if _, err := requireProviderLifecycleKind(kindFilter); err != nil {
			return nil, err
		}
		kindFilter = normalizeProviderLifecycleKind(kindFilter)
	}
	rows := make([]providerLifecycleRow, 0)
	for _, lifecycleKind := range providerLifecycleKinds {
		if kindFilter != "" && lifecycleKind.kind != kindFilter {
			continue
		}
		entries := lifecycleKind.entryMap(cfg)
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		slices.Sort(names)
		lockEntries := lifecycleKind.lockEntries(lock)
		for _, name := range names {
			entry := entries[name]
			if entry == nil {
				continue
			}
			lockEntry, found := lockEntries[name]
			rows = append(rows, providerLifecycleRowFor(lifecycleKind, name, entry, configPath, lockEntry, found))
		}
	}
	return rows, nil
}

func providerLifecycleRowFor(kind providerLifecycleKind, name string, entry *config.ProviderEntry, configPath string, lockEntry operator.LockEntry, lockFound bool) providerLifecycleRow {
	row := providerLifecycleRow{
		Kind:    kind.kind,
		Name:    name,
		Entry:   entry,
		Source:  providerSourceKind(entry),
		Status:  "unlocked",
		Package: entry.Source.PackageAddress(),
		Version: entry.Source.PackageVersionConstraint(),
	}
	if lockFound {
		row.LockedVersion = lockEntry.Version
	}
	if entry.Source.IsBuiltin() {
		row.Status = "builtin"
		return row
	}
	if !providerSourceBacked(entry) {
		return row
	}
	if operator.LockEntryMetadataMatchesProvider(configPath, kind.kind, name, entry, lockEntry, lockFound, kind.kind == providermanifestv1.KindUI) {
		if entry.Source.IsPackage() && entry.Source.ResolvedPackageMetadataURL() == "" {
			row.Status = "unverified"
			return row
		}
		row.Status = "locked"
	} else if lockFound {
		row.Status = "drifted"
	}
	return row
}

func providerSourceKind(entry *config.ProviderEntry) string {
	if entry == nil {
		return ""
	}
	switch {
	case entry.Source.IsPackage():
		return "package"
	case entry.Source.IsBuiltin():
		return "builtin"
	case entry.Source.IsMetadataURL():
		return "metadata"
	case entry.Source.IsGitHubRelease():
		return "github"
	case entry.Source.IsLocal(), entry.Source.IsLocalMetadataPath():
		return "local"
	case entry.Source.UnsupportedURL() != "":
		return "unsupported"
	default:
		return ""
	}
}

func providerSourceBacked(entry *config.ProviderEntry) bool {
	return entry != nil && (entry.Source.IsPackage() || entry.Source.IsMetadataURL() || entry.Source.IsGitHubRelease() || entry.Source.IsLocal() || entry.Source.IsLocalMetadataPath())
}

func readProviderListLockfile(configPath, lockfilePath string) (*operator.Lockfile, error) {
	path := resolveProviderLockfilePath(configPath, lockfilePath)
	lock, err := operator.ReadLockfile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return lock, nil
}

func resolveProviderLockfilePath(configPath, lockfilePath string) string {
	if strings.TrimSpace(lockfilePath) == "" {
		return filepath.Join(filepath.Dir(configPath), operator.LockfileName)
	}
	if filepath.IsAbs(lockfilePath) {
		return lockfilePath
	}
	if abs, err := filepath.Abs(lockfilePath); err == nil {
		return abs
	}
	return lockfilePath
}

func resolveMutableProviderConfig(configPaths []string) (string, []string, error) {
	if len(configPaths) > 1 {
		return "", nil, fmt.Errorf("mutating provider commands accept only one --config; pass the intended writable config as the only --config")
	}
	paths := operator.ResolveConfigPaths(configPaths)
	if len(paths) > 1 {
		return "", nil, fmt.Errorf("mutating provider commands accept only one resolved config; pass the intended writable config as --config")
	}
	primary, err := ensurePrimaryProviderConfig(paths)
	if err != nil {
		return "", nil, err
	}
	return primary, []string{primary}, nil
}

func providerEntryExists(cfg *config.Config, kind, name string) bool {
	lifecycleKind, ok := providerLifecycleKindFor(kind)
	if !ok {
		return false
	}
	_, exists := lifecycleKind.entryMap(cfg)[name]
	return exists
}

type providerMutationPreflight struct {
	ConfigPath string
	Root       *yaml.Node
	Lock       *operator.Lockfile
	LockPath   string
}

func preflightProviderConfigMutation(configPath string, root *yaml.Node, lockfilePath string, noLock bool) (*providerMutationPreflight, error) {
	data, err := configDocumentBytes(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tempConfig, err := os.CreateTemp(dir, "."+filepath.Base(configPath)+".preflight-*")
	if err != nil {
		return nil, err
	}
	tempConfigPath := tempConfig.Name()
	defer func() { _ = os.Remove(tempConfigPath) }()
	if _, err := tempConfig.Write(data); err != nil {
		_ = tempConfig.Close()
		return nil, err
	}
	if err := tempConfig.Close(); err != nil {
		return nil, err
	}
	if noLock {
		if _, err := config.LoadAllowMissingEnvPaths([]string{tempConfigPath}); err != nil {
			return nil, err
		}
		return &providerMutationPreflight{ConfigPath: configPath, Root: root}, nil
	}
	scratchDir, err := os.MkdirTemp("", "gestaltd-provider-preflight-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(scratchDir) }()
	scratchState := operator.StatePaths{
		ArtifactsDir: filepath.Join(scratchDir, "artifacts"),
		LockfilePath: filepath.Join(scratchDir, operator.LockfileName),
	}
	lock, err := operatorLifecycle().LockAtPathsWithStatePaths([]string{tempConfigPath}, scratchState)
	if err != nil {
		return nil, err
	}
	return &providerMutationPreflight{
		ConfigPath: configPath,
		Root:       root,
		Lock:       lock,
		LockPath:   resolveProviderLockfilePath(configPath, lockfilePath),
	}, nil
}

func commitProviderConfigMutation(preflight *providerMutationPreflight) (providerMutationResult, error) {
	if preflight == nil {
		return providerMutationResult{}, fmt.Errorf("provider mutation preflight is required")
	}
	result := providerMutationResult{ConfigPath: preflight.ConfigPath}
	configData, err := configDocumentBytes(preflight.Root)
	if err != nil {
		return result, err
	}
	previousConfig, readErr := os.ReadFile(preflight.ConfigPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		return result, readErr
	}
	if err := writeFileAtomic(preflight.ConfigPath, configData, 0o600); err != nil {
		return result, err
	}
	restoreConfig := func() error {
		if readErr == nil {
			return writeFileAtomic(preflight.ConfigPath, previousConfig, 0o600)
		}
		return os.Remove(preflight.ConfigPath)
	}
	if preflight.Lock != nil {
		result.LockfilePath = preflight.LockPath
		if err := writeLockfileAtomic(preflight.LockPath, preflight.Lock); err != nil {
			if restoreErr := restoreConfig(); restoreErr != nil {
				return result, fmt.Errorf("%w; additionally failed to restore config %s: %v", err, preflight.ConfigPath, restoreErr)
			}
			return result, err
		}
		result.LockWritten = true
	}
	return result, nil
}

func configDocumentBytes(root *yaml.Node) ([]byte, error) {
	data, err := yaml.Marshal(root)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func writeLockfileAtomic(path string, lock *operator.Lockfile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := operator.WriteLockfile(tempPath, lock); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Chmod(perm); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func providerRemoveTargets(doc *yaml.Node, kind, name string) ([]providerEntryCollectionRef, error) {
	if strings.TrimSpace(kind) != "" {
		lifecycleKind, err := requireProviderLifecycleKind(kind)
		if err != nil {
			return nil, err
		}
		collection := lifecycleKind.collection(doc)
		if mappingValueNodeLocal(collection, name) == nil {
			return nil, fmt.Errorf("provider %q of kind %q not found", name, lifecycleKind.kind)
		}
		return []providerEntryCollectionRef{{kind: lifecycleKind.kind, node: collection}}, nil
	}
	targets := make([]providerEntryCollectionRef, 0, 1)
	for _, lifecycleKind := range providerLifecycleKinds {
		collection := lifecycleKind.collection(doc)
		if mappingValueNodeLocal(collection, name) != nil {
			targets = append(targets, providerEntryCollectionRef{kind: lifecycleKind.kind, node: collection})
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	if len(targets) > 1 {
		kinds := make([]string, 0, len(targets))
		for _, target := range targets {
			kinds = append(kinds, target.kind)
		}
		slices.Sort(kinds)
		return nil, fmt.Errorf("provider %q is ambiguous across kinds %s; pass --kind", name, strings.Join(kinds, ", "))
	}
	return targets, nil
}

func applyProviderEntry(doc *yaml.Node, apiVersion, kind, name string, resolved *providerregistry.ResolvedPackage, constraint, repoName string, packageSource bool, setValues map[string]string) error {
	if _, err := requireProviderLifecycleKind(kind); err != nil {
		return err
	}
	setScalar(doc, "apiVersion", apiVersion)
	entry := map[string]any{}
	if packageSource {
		source := map[string]any{"package": resolved.Package}
		sourceRepoName := ""
		if repoName != "" {
			sourceRepoName = repoName
		} else if resolved.RepositoryName != providerregistry.DefaultRepositoryName {
			sourceRepoName = resolved.RepositoryName
		}
		if sourceRepoName != "" {
			source["repo"] = sourceRepoName
			if resolved.RepositoryURL != "" {
				repos := ensureMapping(doc, "providerRepositories")
				setNode(repos, sourceRepoName, yamlMapping(map[string]any{"url": resolved.RepositoryURL}))
			}
		}
		if constraint != "" {
			source["version"] = constraint
		}
		entry["source"] = source
	} else {
		entry["source"] = resolved.MetadataURL
	}
	if kind == providermanifestv1.KindUI {
		entry["path"] = setValues["path"]
	}
	target := providerEntryCollection(doc, kind)
	setNode(target, name, yamlMapping(entry))
	return nil
}
