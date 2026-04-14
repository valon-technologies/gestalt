package config

import (
	"fmt"
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
	return c.SelectedHostProvider(HostProviderKindIndexedDB)
}

func (c *Config) EffectivePluginIndexedDBBindings(plugin *ProviderEntry) ([]string, error) {
	c.SyncCompatFields()
	selectedName, _, err := c.SelectedIndexedDBProvider()
	if err != nil {
		return nil, err
	}
	return ResolveEffectivePluginIndexedDBBindings(plugin, selectedName), nil
}

func ResolveEffectivePluginIndexedDBBindings(plugin *ProviderEntry, selectedName string) []string {
	if plugin == nil {
		return nil
	}
	if plugin.IndexedDBs != nil {
		bindings := make([]string, len(plugin.IndexedDBs))
		for i, binding := range plugin.IndexedDBs {
			bindings[i] = strings.TrimSpace(binding)
		}
		return bindings
	}
	selectedName = strings.TrimSpace(selectedName)
	if selectedName == "" {
		return nil
	}
	return []string{selectedName}
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
