package bootstrap

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/server/core/indexeddb"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/observability/metricutil"
	"gopkg.in/yaml.v3"
)

func buildPluginScopedIndexedDB(_ string, effective config.EffectiveHostIndexedDBBinding, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   effective.ProviderName,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

func buildWorkflowScopedIndexedDB(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   name,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

func buildAgentScopedIndexedDB(name string, effective config.EffectiveHostIndexedDBBinding, deps Deps) (indexeddb.IndexedDB, error) {
	return buildScopedIndexedDB(scopedIndexedDBBuildOptions{
		MetricsName:   name,
		ProviderName:  effective.ProviderName,
		DB:            effective.DB,
		AllowedStores: effective.ObjectStores,
	}, deps)
}

type scopedIndexedDBBuildOptions struct {
	MetricsName   string
	ProviderName  string
	DB            string
	AllowedStores []string
}

func buildScopedIndexedDB(opts scopedIndexedDBBuildOptions, deps Deps) (indexeddb.IndexedDB, error) {
	def, ok := deps.IndexedDBDefs[opts.ProviderName]
	if !ok || def == nil {
		return nil, fmt.Errorf("indexeddb %q is not available", opts.ProviderName)
	}
	scopedDef, err := newScopedIndexedDBDef(def, scopedIndexedDBDefOptions{
		DB: opts.DB,
	})
	if err != nil {
		return nil, fmt.Errorf("indexeddb %q: %w", opts.ProviderName, err)
	}
	ds, err := buildIndexedDB(scopedDef, &FactoryRegistry{IndexedDB: deps.IndexedDBFactory})
	if err != nil {
		return nil, fmt.Errorf("indexeddb %q: %w", opts.ProviderName, err)
	}
	ds = newIndexedDBStoreAllowlist(ds, indexedDBStoreAllowlistOptions{
		AllowedStores: opts.AllowedStores,
	})
	return metricutil.InstrumentIndexedDB(ds, opts.MetricsName), nil
}

type scopedIndexedDBDefOptions struct {
	DB string
}

func newScopedIndexedDBDef(entry *config.ProviderEntry, opts scopedIndexedDBDefOptions) (*config.ProviderEntry, error) {
	if entry == nil {
		return nil, fmt.Errorf("datastore provider is required")
	}
	cfg, err := config.NodeToMap(entry.Config)
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}

	switch {
	case isRelationalIndexedDBEntry(entry):
		if isSQLiteIndexedDBConfig(cfg) {
			delete(cfg, "schema")
			cfg["table_prefix"] = opts.DB + "_"
			cfg["prefix"] = opts.DB + "_"
		} else {
			delete(cfg, "table_prefix")
			delete(cfg, "prefix")
			cfg["schema"] = opts.DB
		}
	case isMongoDBIndexedDBEntry(entry):
		cfg["database"] = opts.DB
	case isDynamoDBIndexedDBEntry(entry):
		cfg["table"] = opts.DB
	default:
		return nil, fmt.Errorf("scoped indexeddb bindings require a provider with config-level namespace support")
	}

	configNode, err := mapToYAMLNode(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}

	cloned := *entry
	cloned.Config = configNode
	return &cloned, nil
}

func isRelationalIndexedDBEntry(entry *config.ProviderEntry) bool {
	return isIndexedDBProviderEntry(entry, "relationaldb")
}

func isMongoDBIndexedDBEntry(entry *config.ProviderEntry) bool {
	return isIndexedDBProviderEntry(entry, "mongodb")
}

func isDynamoDBIndexedDBEntry(entry *config.ProviderEntry) bool {
	return isIndexedDBProviderEntry(entry, "dynamodb")
}

func isIndexedDBProviderEntry(entry *config.ProviderEntry, providerName string) bool {
	if entry == nil {
		return false
	}
	providerPath := "/indexeddb/" + providerName
	if entry.ResolvedManifest != nil {
		source := strings.TrimSpace(filepath.ToSlash(entry.ResolvedManifest.Source))
		return strings.HasSuffix(source, providerPath)
	}
	if metadataURL := strings.TrimSpace(entry.SourceMetadataURL()); metadataURL != "" {
		parsed, err := url.Parse(metadataURL)
		if err == nil {
			path := filepath.ToSlash(parsed.Path)
			return strings.Contains(path, providerPath+"/") && strings.HasSuffix(path, "/provider-release.yaml")
		}
	}
	if path := strings.TrimSpace(entry.SourcePath()); path != "" {
		path = filepath.ToSlash(path)
		return strings.HasSuffix(path, providerPath) ||
			strings.HasSuffix(path, "/"+providerName) ||
			strings.HasSuffix(path, providerPath+"/manifest.yaml") ||
			strings.HasSuffix(path, "/"+providerName+"/manifest.yaml")
	}
	return false
}

func isSQLiteIndexedDBConfig(cfg map[string]any) bool {
	dsn, _ := cfg["dsn"].(string)
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return false
	}
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return false
	case strings.HasPrefix(dsn, "mysql://"), strings.Contains(dsn, "@tcp("), strings.Contains(dsn, "@unix("):
		return false
	case strings.HasPrefix(dsn, "sqlserver://"):
		return false
	default:
		return true
	}
}

func mapToYAMLNode(value map[string]any) (yaml.Node, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return yaml.Node{}, err
	}
	var out yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&out); err != nil {
		return yaml.Node{}, err
	}
	if out.Kind == yaml.DocumentNode && len(out.Content) == 1 {
		return *out.Content[0], nil
	}
	return out, nil
}
