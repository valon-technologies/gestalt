package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

func (c *Config) SyncCompatFields() {
	if c == nil {
		return
	}
	if c.Plugins == nil && c.Providers.Plugins != nil {
		c.Plugins = c.Providers.Plugins
	}
	if c.Providers.Plugins == nil && c.Plugins != nil {
		c.Providers.Plugins = c.Plugins
	}
	if c.Providers.IndexedDB == nil && c.Providers.IndexedDBs != nil {
		c.Providers.IndexedDB = c.Providers.IndexedDBs
	}
	if c.Providers.IndexedDBs == nil && c.Providers.IndexedDB != nil {
		c.Providers.IndexedDBs = c.Providers.IndexedDB
	}
	if c.Server.Providers.IndexedDB == "" && c.Server.IndexedDB != "" {
		c.Server.Providers.IndexedDB = c.Server.IndexedDB
	}
	if c.Server.IndexedDB == "" && c.Server.Providers.IndexedDB != "" {
		c.Server.IndexedDB = c.Server.Providers.IndexedDB
	}
}

func (s ServerProvidersConfig) Selection(kind HostProviderKind) string {
	switch kind {
	case HostProviderKindAuth:
		return s.Auth
	case HostProviderKindSecrets:
		return s.Secrets
	case HostProviderKindTelemetry:
		return s.Telemetry
	case HostProviderKindAudit:
		return s.Audit
	case HostProviderKindIndexedDB:
		return s.IndexedDB
	default:
		return ""
	}
}

func (c *Config) HostProviderEntries(kind HostProviderKind) map[string]*ProviderEntry {
	if c == nil {
		return nil
	}
	c.SyncCompatFields()
	switch kind {
	case HostProviderKindAuth:
		return c.Providers.Auth
	case HostProviderKindSecrets:
		return c.Providers.Secrets
	case HostProviderKindTelemetry:
		return c.Providers.Telemetry
	case HostProviderKindAudit:
		return c.Providers.Audit
	case HostProviderKindIndexedDB:
		return c.Providers.IndexedDB
	default:
		return nil
	}
}

func (c *Config) SelectedHostProvider(kind HostProviderKind) (string, *ProviderEntry, error) {
	c.SyncCompatFields()
	return ResolveSelectedHostProvider(kind, c.Server.Providers.Selection(kind), c.HostProviderEntries(kind))
}

func (c *Config) SelectedAuthProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindAuth)
}

func (c *Config) SelectedSecretsProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindSecrets)
}

func (c *Config) SelectedTelemetryProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindTelemetry)
}

func (c *Config) SelectedAuditProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindAudit)
}

func (c *Config) SelectedIndexedDBProvider() (string, *ProviderEntry, error) {
	c.SyncCompatFields()
	name, entry, err := ResolveSelectedHostProvider(HostProviderKindIndexedDB, c.Server.Providers.Selection(HostProviderKindIndexedDB), c.HostProviderEntries(HostProviderKindIndexedDB))
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(c.Server.Providers.IndexedDB) == "" && entry != nil && entry.Disabled {
		return "", nil, nil
	}
	return name, entry, nil
}

type EffectivePluginIndexedDB struct {
	Enabled      bool
	ProviderName string
	Provider     *ProviderEntry
	DB           string
	ObjectStores []string
}

func (c *Config) EffectivePluginIndexedDB(pluginName string, entry *ProviderEntry) (EffectivePluginIndexedDB, error) {
	c.SyncCompatFields()
	selectedName, _, err := c.SelectedIndexedDBProvider()
	if err != nil {
		return EffectivePluginIndexedDB{}, err
	}
	return ResolveEffectivePluginIndexedDB(pluginName, entry, selectedName, c.Providers.IndexedDB)
}

func ResolveEffectivePluginIndexedDB(pluginName string, entry *ProviderEntry, selectedName string, entries map[string]*ProviderEntry) (EffectivePluginIndexedDB, error) {
	if entry == nil {
		return EffectivePluginIndexedDB{}, nil
	}
	if entry.IndexedDB != nil && entry.IndexedDB.Disabled {
		return EffectivePluginIndexedDB{}, nil
	}

	providerName := ""
	if entry.IndexedDB != nil {
		providerName = strings.TrimSpace(entry.IndexedDB.Provider)
	}
	if providerName == "" {
		providerName = strings.TrimSpace(selectedName)
	}
	if providerName == "" {
		if entry.IndexedDB != nil && (strings.TrimSpace(entry.IndexedDB.DB) != "" || len(entry.IndexedDB.ObjectStores) > 0) {
			return EffectivePluginIndexedDB{}, fmt.Errorf("config validation: plugins.%s.indexeddb requires indexeddb.provider or an available selected/default host indexeddb", pluginName)
		}
		return EffectivePluginIndexedDB{}, nil
	}

	provider, ok := entries[providerName]
	if !ok || provider == nil {
		return EffectivePluginIndexedDB{}, fmt.Errorf("config validation: plugins.%s.indexeddb.provider references unknown indexeddb %q", pluginName, providerName)
	}
	if provider.Disabled {
		return EffectivePluginIndexedDB{}, fmt.Errorf("config validation: plugins.%s.indexeddb.provider references disabled indexeddb %q", pluginName, providerName)
	}

	dbName := pluginName
	if entry.IndexedDB != nil && strings.TrimSpace(entry.IndexedDB.DB) != "" {
		dbName = strings.TrimSpace(entry.IndexedDB.DB)
	} else if legacyDB := strings.TrimSpace(entry.IndexedDBSchema); legacyDB != "" {
		dbName = legacyDB
	}

	var objectStores []string
	if entry.IndexedDB != nil {
		objectStores = slices.Clone(entry.IndexedDB.ObjectStores)
	}

	return EffectivePluginIndexedDB{
		Enabled:      true,
		ProviderName: providerName,
		Provider:     provider,
		DB:           dbName,
		ObjectStores: objectStores,
	}, nil
}

func ResolveSelectedHostProvider(kind HostProviderKind, explicit string, entries map[string]*ProviderEntry) (string, *ProviderEntry, error) {
	if len(entries) == 0 {
		return "", nil, nil
	}
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		entry, ok := entries[explicit]
		if !ok || entry == nil {
			return "", nil, fmt.Errorf("config validation: server.providers.%s references unknown provider %q", kind, explicit)
		}
		if entry.Disabled {
			return "", nil, fmt.Errorf("config validation: server.providers.%s references disabled provider %q", kind, explicit)
		}
		return explicit, entry, nil
	}

	allNames := make([]string, 0, len(entries))
	enabledNames := make([]string, 0, len(entries))
	defaultNames := make([]string, 0, len(entries))
	for name, entry := range entries {
		if entry == nil {
			continue
		}
		allNames = append(allNames, name)
		if entry.Default {
			defaultNames = append(defaultNames, name)
		}
		if entry.Disabled {
			continue
		}
		enabledNames = append(enabledNames, name)
	}

	switch {
	case len(defaultNames) == 1:
		name := defaultNames[0]
		if entries[name].Disabled {
			return "", nil, fmt.Errorf("config validation: providers.%s.%s cannot be both default and disabled", kind, name)
		}
		return name, entries[name], nil
	case len(defaultNames) > 1:
		sort.Strings(defaultNames)
		return "", nil, fmt.Errorf("config validation: providers.%s declares multiple defaults: %s", kind, strings.Join(defaultNames, ", "))
	case len(enabledNames) == 1:
		name := enabledNames[0]
		return name, entries[name], nil
	case len(allNames) == 1:
		name := allNames[0]
		return name, entries[name], nil
	case len(enabledNames) == 0:
		return "", nil, nil
	default:
		sort.Strings(enabledNames)
		return "", nil, fmt.Errorf("config validation: providers.%s has multiple enabled providers but no selection or default: %s", kind, strings.Join(enabledNames, ", "))
	}
}
