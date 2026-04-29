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
	case HostProviderKindAuthentication:
		return s.Authentication
	case HostProviderKindAuthorization:
		return s.Authorization
	case HostProviderKindExternalCredentials:
		return s.ExternalCredentials
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
	case HostProviderKindAgent:
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
	case HostProviderKindAuthentication:
		return c.Providers.Authentication
	case HostProviderKindAuthorization:
		return c.Providers.Authorization
	case HostProviderKindExternalCredentials:
		return c.Providers.ExternalCredentials
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
	case HostProviderKindAgent:
		return c.Providers.Agent
	default:
		return nil
	}
}

func (c *Config) SelectedHostProvider(kind HostProviderKind) (string, *ProviderEntry, error) {
	return ResolveSelectedHostProvider(kind, c.Server.Providers.Selection(kind), c.HostProviderEntries(kind))
}

func (c *Config) SelectedAuthenticationProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindAuthentication)
}

func (c *Config) SelectedAuthorizationProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindAuthorization)
}

func (c *Config) SelectedExternalCredentialsProvider() (string, *ProviderEntry, error) {
	return c.SelectedHostProvider(HostProviderKindExternalCredentials)
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

func (c *Config) SelectedAgentProvider() (string, *ProviderEntry, error) {
	return ResolveSelectedHostProvider(HostProviderKindAgent, "", c.HostProviderEntries(HostProviderKindAgent))
}

func (c *Config) SelectedRuntimeProvider() (string, *RuntimeProviderEntry, error) {
	if c == nil {
		return "", nil, nil
	}
	return ResolveSelectedRuntimeProvider(c.Server.Runtime.SelectedDefaultHostedProvider(), c.Runtime.Providers)
}

type EffectiveHostIndexedDBBinding struct {
	Enabled      bool
	ProviderName string
	Provider     *ProviderEntry
	DB           string
	ObjectStores []string
}

type EffectiveHostedRuntime struct {
	Enabled              bool
	ProviderName         string
	Provider             *RuntimeProviderEntry
	Template             string
	Image                string
	ImagePullCredentials *HostedRuntimeImagePullCredentials
	Metadata             map[string]string
}

type EffectiveExecution struct {
	Mode   ExecutionMode
	Hosted EffectiveHostedRuntime
}

func (c *Config) EffectivePluginIndexedDB(pluginName string, entry *ProviderEntry) (EffectiveHostIndexedDBBinding, error) {
	selectedName, _, err := c.SelectedIndexedDBProvider()
	if err != nil {
		return EffectiveHostIndexedDBBinding{}, err
	}
	return ResolveEffectivePluginIndexedDB(pluginName, entry, selectedName, c.Providers.IndexedDB)
}

func (c *Config) EffectiveWorkflowProvider(providerName string) (string, *ProviderEntry, error) {
	if c == nil {
		return "", nil, nil
	}
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		entry, ok := c.Providers.Workflow[providerName]
		if !ok || entry == nil {
			return "", nil, fmt.Errorf("config validation: providers.workflow references unknown workflow %q", providerName)
		}
		return providerName, entry, nil
	}
	return c.SelectedWorkflowProvider()
}

func (c *Config) EffectiveAgentProvider(providerName string) (string, *ProviderEntry, error) {
	if c == nil {
		return "", nil, nil
	}
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		entry, ok := c.Providers.Agent[providerName]
		if !ok || entry == nil {
			return "", nil, fmt.Errorf("config validation: providers.agent references unknown agent %q", providerName)
		}
		return providerName, entry, nil
	}
	return c.SelectedAgentProvider()
}

func (c *Config) EffectiveWorkflowIndexedDB(name string, entry *ProviderEntry) (EffectiveHostIndexedDBBinding, error) {
	return ResolveEffectiveWorkflowIndexedDB(name, entry, c.Providers.IndexedDB)
}

func (c *Config) EffectiveAgentIndexedDB(name string, entry *ProviderEntry) (EffectiveHostIndexedDBBinding, error) {
	return ResolveEffectiveAgentIndexedDB(name, entry, c.Providers.IndexedDB)
}

func (c *Config) EffectiveHostedRuntime(configPath string, entry *ProviderEntry) (EffectiveHostedRuntime, error) {
	execution, err := c.EffectiveExecution(configPath, entry)
	if err != nil {
		return EffectiveHostedRuntime{}, err
	}
	return execution.Hosted, nil
}

func (c *Config) EffectiveExecution(configPath string, entry *ProviderEntry) (EffectiveExecution, error) {
	if c == nil {
		return EffectiveExecution{Mode: ExecutionModeLocal}, nil
	}
	selectedName, _, err := c.SelectedRuntimeProvider()
	if err != nil {
		return EffectiveExecution{}, err
	}
	return ResolveEffectiveExecution(configPath, entry, selectedName, c.Runtime.Providers)
}

func ResolveEffectivePluginIndexedDB(pluginName string, entry *ProviderEntry, selectedName string, entries map[string]*ProviderEntry) (EffectiveHostIndexedDBBinding, error) {
	if entry == nil {
		return EffectiveHostIndexedDBBinding{}, nil
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
			return EffectiveHostIndexedDBBinding{}, fmt.Errorf("config validation: plugins.%s.indexeddb requires indexeddb.provider or an available selected/default host indexeddb", pluginName)
		}
		return EffectiveHostIndexedDBBinding{}, nil
	}

	provider, ok := entries[providerName]
	if !ok || provider == nil {
		return EffectiveHostIndexedDBBinding{}, fmt.Errorf("config validation: plugins.%s.indexeddb.provider references unknown indexeddb %q", pluginName, providerName)
	}

	dbName := pluginName
	if entry.IndexedDB != nil && strings.TrimSpace(entry.IndexedDB.DB) != "" {
		dbName = strings.TrimSpace(entry.IndexedDB.DB)
	}

	var objectStores []string
	if entry.IndexedDB != nil {
		objectStores = slices.Clone(entry.IndexedDB.ObjectStores)
	}

	return EffectiveHostIndexedDBBinding{
		Enabled:      true,
		ProviderName: providerName,
		Provider:     provider,
		DB:           dbName,
		ObjectStores: objectStores,
	}, nil
}

func ResolveEffectiveWorkflowIndexedDB(name string, entry *ProviderEntry, entries map[string]*ProviderEntry) (EffectiveHostIndexedDBBinding, error) {
	if entry == nil || entry.IndexedDB == nil {
		return EffectiveHostIndexedDBBinding{}, nil
	}

	providerName := strings.TrimSpace(entry.IndexedDB.Provider)
	if providerName == "" {
		return EffectiveHostIndexedDBBinding{}, fmt.Errorf("config validation: providers.workflow.%s.indexeddb.provider is required", name)
	}

	provider, ok := entries[providerName]
	if !ok || provider == nil {
		return EffectiveHostIndexedDBBinding{}, fmt.Errorf("config validation: providers.workflow.%s.indexeddb.provider references unknown indexeddb %q", name, providerName)
	}

	dbName := strings.TrimSpace(entry.IndexedDB.DB)
	if dbName == "" {
		dbName = name
	}

	return EffectiveHostIndexedDBBinding{
		Enabled:      true,
		ProviderName: providerName,
		Provider:     provider,
		DB:           dbName,
		ObjectStores: slices.Clone(entry.IndexedDB.ObjectStores),
	}, nil
}

func ResolveEffectiveAgentIndexedDB(name string, entry *ProviderEntry, entries map[string]*ProviderEntry) (EffectiveHostIndexedDBBinding, error) {
	if entry == nil || entry.IndexedDB == nil {
		return EffectiveHostIndexedDBBinding{}, nil
	}

	providerName := strings.TrimSpace(entry.IndexedDB.Provider)
	if providerName == "" {
		return EffectiveHostIndexedDBBinding{}, fmt.Errorf("config validation: providers.agent.%s.indexeddb.provider is required", name)
	}

	provider, ok := entries[providerName]
	if !ok || provider == nil {
		return EffectiveHostIndexedDBBinding{}, fmt.Errorf("config validation: providers.agent.%s.indexeddb.provider references unknown indexeddb %q", name, providerName)
	}

	dbName := strings.TrimSpace(entry.IndexedDB.DB)
	if dbName == "" {
		dbName = name
	}

	return EffectiveHostIndexedDBBinding{
		Enabled:      true,
		ProviderName: providerName,
		Provider:     provider,
		DB:           dbName,
		ObjectStores: slices.Clone(entry.IndexedDB.ObjectStores),
	}, nil
}

func ResolveEffectiveHostedRuntime(configPath string, entry *ProviderEntry, selectedName string, entries map[string]*RuntimeProviderEntry) (EffectiveHostedRuntime, error) {
	execution, err := ResolveEffectiveExecution(configPath, entry, selectedName, entries)
	if err != nil {
		return EffectiveHostedRuntime{}, err
	}
	return execution.Hosted, nil
}

func ResolveEffectiveExecution(configPath string, entry *ProviderEntry, selectedName string, entries map[string]*RuntimeProviderEntry) (EffectiveExecution, error) {
	if entry == nil {
		return EffectiveExecution{Mode: ExecutionModeLocal}, nil
	}
	mode := ExecutionModeLocal
	providerPath := "execution.runtime.provider"
	var runtimeCfg *HostedRuntimeConfig
	if entry.Execution != nil {
		mode = entry.Execution.Mode
		if mode == "" {
			if entry.Execution.Runtime != nil {
				mode = ExecutionModeHosted
			} else {
				mode = ExecutionModeLocal
			}
		}
		runtimeCfg = entry.Execution.Runtime
	}
	switch mode {
	case "", ExecutionModeLocal:
		return EffectiveExecution{Mode: ExecutionModeLocal}, nil
	case ExecutionModeHosted:
	default:
		return EffectiveExecution{}, fmt.Errorf("config validation: %s.execution.mode must be %q or %q, got %q", configPath, ExecutionModeLocal, ExecutionModeHosted, mode)
	}
	if runtimeCfg == nil {
		runtimeCfg = &HostedRuntimeConfig{}
	}

	providerName := strings.TrimSpace(runtimeCfg.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(selectedName)
	}

	var provider *RuntimeProviderEntry
	if providerName != "" {
		var ok bool
		provider, ok = entries[providerName]
		if !ok || provider == nil {
			return EffectiveExecution{}, fmt.Errorf("config validation: %s.%s references unknown runtime %q", configPath, providerPath, providerName)
		}
	}

	runtime := EffectiveHostedRuntime{
		Enabled:              true,
		ProviderName:         providerName,
		Provider:             provider,
		Template:             strings.TrimSpace(runtimeCfg.Template),
		Image:                strings.TrimSpace(runtimeCfg.Image),
		ImagePullCredentials: cloneHostedRuntimeImagePullCredentials(runtimeCfg.ImagePullCredentials),
		Metadata:             maps.Clone(runtimeCfg.Metadata),
	}
	return EffectiveExecution{Mode: ExecutionModeHosted, Hosted: runtime}, nil
}

func (s ServerRuntimeConfig) SelectedDefaultHostedProvider() string {
	return strings.TrimSpace(s.DefaultHostedProvider)
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

func ResolveSelectedRuntimeProvider(explicit string, entries map[string]*RuntimeProviderEntry) (string, *RuntimeProviderEntry, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if len(entries) == 0 {
			return "", nil, fmt.Errorf("config validation: server.runtime.defaultHostedProvider references unknown runtime %q", explicit)
		}
		entry, ok := entries[explicit]
		if !ok || entry == nil {
			return "", nil, fmt.Errorf("config validation: server.runtime.defaultHostedProvider references unknown runtime %q", explicit)
		}
		return explicit, entry, nil
	}
	if len(entries) == 0 {
		return "", nil, nil
	}

	defaultNames := make([]string, 0, len(entries))
	for name, entry := range entries {
		if entry == nil {
			continue
		}
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
		return "", nil, fmt.Errorf("config validation: runtime.providers declares multiple defaults: %s", strings.Join(defaultNames, ", "))
	default:
		return "", nil, nil
	}
}
