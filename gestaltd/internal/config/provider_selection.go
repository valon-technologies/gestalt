package config

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

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
	case HostProviderKindCache:
		return ""
	case HostProviderKindWorkflow:
		return ""
	default:
		return ""
	}
}

func (c *Config) HostProviderEntries(kind HostProviderKind) map[string]*ProviderEntry {
	if c == nil {
		return nil
	}
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
	case HostProviderKindCache:
		return c.Providers.Cache
	case HostProviderKindWorkflow:
		return c.Providers.Workflow
	default:
		return nil
	}
}

func (c *Config) SelectedHostProvider(kind HostProviderKind) (string, *ProviderEntry, error) {
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
	return ResolveSelectedHostProvider(HostProviderKindIndexedDB, c.Server.Providers.Selection(HostProviderKindIndexedDB), c.HostProviderEntries(HostProviderKindIndexedDB))
}

func (c *Config) SelectedWorkflowProvider() (string, *ProviderEntry, error) {
	return ResolveSelectedHostProvider(HostProviderKindWorkflow, "", c.HostProviderEntries(HostProviderKindWorkflow))
}

type EffectivePluginIndexedDB struct {
	Enabled      bool
	ProviderName string
	Provider     *ProviderEntry
	DB           string
	ObjectStores []string
}

type EffectiveWorkflowIndexedDB struct {
	Enabled      bool
	ProviderName string
	Provider     *ProviderEntry
	DB           string
	ObjectStores []string
}

type EffectivePluginWorkflow struct {
	Enabled       bool
	ProviderName  string
	Provider      *ProviderEntry
	Operations    []string
	Schedules     map[string]PluginWorkflowSchedule
	EventTriggers map[string]PluginWorkflowEventTrigger
}

func (c *Config) EffectivePluginIndexedDB(pluginName string, entry *ProviderEntry) (EffectivePluginIndexedDB, error) {
	selectedName, _, err := c.SelectedIndexedDBProvider()
	if err != nil {
		return EffectivePluginIndexedDB{}, err
	}
	return ResolveEffectivePluginIndexedDB(pluginName, entry, selectedName, c.Providers.IndexedDB)
}

func (c *Config) EffectivePluginWorkflow(pluginName string, entry *ProviderEntry) (EffectivePluginWorkflow, error) {
	selectedName := ""
	if entry != nil && entry.Workflow != nil && strings.TrimSpace(entry.Workflow.Provider) == "" {
		var err error
		selectedName, _, err = c.SelectedWorkflowProvider()
		if err != nil {
			return EffectivePluginWorkflow{}, err
		}
	}
	return ResolveEffectivePluginWorkflow(pluginName, entry, selectedName, c.Providers.Workflow)
}

func (c *Config) EffectiveWorkflowIndexedDB(name string, entry *ProviderEntry) (EffectiveWorkflowIndexedDB, error) {
	return ResolveEffectiveWorkflowIndexedDB(name, entry, c.Providers.IndexedDB)
}

func ResolveEffectivePluginIndexedDB(pluginName string, entry *ProviderEntry, selectedName string, entries map[string]*ProviderEntry) (EffectivePluginIndexedDB, error) {
	if entry == nil {
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

	dbName := pluginName
	if entry.IndexedDB != nil && strings.TrimSpace(entry.IndexedDB.DB) != "" {
		dbName = strings.TrimSpace(entry.IndexedDB.DB)
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

func ResolveEffectiveWorkflowIndexedDB(name string, entry *ProviderEntry, entries map[string]*ProviderEntry) (EffectiveWorkflowIndexedDB, error) {
	if entry == nil || entry.IndexedDB == nil {
		return EffectiveWorkflowIndexedDB{}, nil
	}

	providerName := strings.TrimSpace(entry.IndexedDB.Provider)
	if providerName == "" {
		return EffectiveWorkflowIndexedDB{}, fmt.Errorf("config validation: providers.workflow.%s.indexeddb.provider is required", name)
	}

	provider, ok := entries[providerName]
	if !ok || provider == nil {
		return EffectiveWorkflowIndexedDB{}, fmt.Errorf("config validation: providers.workflow.%s.indexeddb.provider references unknown indexeddb %q", name, providerName)
	}

	dbName := strings.TrimSpace(entry.IndexedDB.DB)
	if dbName == "" {
		dbName = name
	}

	return EffectiveWorkflowIndexedDB{
		Enabled:      true,
		ProviderName: providerName,
		Provider:     provider,
		DB:           dbName,
		ObjectStores: slices.Clone(entry.IndexedDB.ObjectStores),
	}, nil
}

func ResolveEffectivePluginWorkflow(pluginName string, entry *ProviderEntry, selectedName string, entries map[string]*ProviderEntry) (EffectivePluginWorkflow, error) {
	if entry == nil || entry.Workflow == nil {
		return EffectivePluginWorkflow{}, nil
	}

	providerName := strings.TrimSpace(entry.Workflow.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(selectedName)
	}
	if providerName == "" {
		return EffectivePluginWorkflow{}, fmt.Errorf("config validation: plugins.%s.workflow requires workflow.provider or a selected/default providers.workflow entry", pluginName)
	}

	provider, ok := entries[providerName]
	if !ok || provider == nil {
		return EffectivePluginWorkflow{}, fmt.Errorf("config validation: plugins.%s.workflow.provider references unknown workflow %q", pluginName, providerName)
	}

	return EffectivePluginWorkflow{
		Enabled:       true,
		ProviderName:  providerName,
		Provider:      provider,
		Operations:    slices.Clone(entry.Workflow.Operations),
		Schedules:     clonePluginWorkflowSchedules(entry.Workflow.Schedules),
		EventTriggers: clonePluginWorkflowEventTriggers(entry.Workflow.EventTriggers),
	}, nil
}

func clonePluginWorkflowSchedules(src map[string]PluginWorkflowSchedule) map[string]PluginWorkflowSchedule {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]PluginWorkflowSchedule, len(src))
	for key, schedule := range src {
		schedule.Input = maps.Clone(schedule.Input)
		dst[key] = schedule
	}
	return dst
}

func clonePluginWorkflowEventTriggers(src map[string]PluginWorkflowEventTrigger) map[string]PluginWorkflowEventTrigger {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]PluginWorkflowEventTrigger, len(src))
	for key, trigger := range src {
		trigger.Input = maps.Clone(trigger.Input)
		dst[key] = trigger
	}
	return dst
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
		return explicit, entry, nil
	}

	names := make([]string, 0, len(entries))
	defaultNames := make([]string, 0, len(entries))
	for name, entry := range entries {
		if entry == nil {
			continue
		}
		names = append(names, name)
		if entry.Default {
			defaultNames = append(defaultNames, name)
		}
	}

	switch {
	case len(defaultNames) == 1:
		name := defaultNames[0]
		return name, entries[name], nil
	case len(defaultNames) > 1:
		sort.Strings(defaultNames)
		return "", nil, fmt.Errorf("config validation: providers.%s declares multiple defaults: %s", kind, strings.Join(defaultNames, ", "))
	case len(names) == 1:
		name := names[0]
		return name, entries[name], nil
	default:
		sort.Strings(names)
		return "", nil, fmt.Errorf("config validation: providers.%s has multiple providers but no selection or default: %s", kind, strings.Join(names, ", "))
	}
}
