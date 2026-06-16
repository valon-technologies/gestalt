package config

import (
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

// ExternalCredentialsReferenced reports whether runtime needs an external
// credentials provider.
func ExternalCredentialsReferenced(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.Server.Providers.ExternalCredentials) != "" {
		return true
	}
	if externalCredentialsUsageReferenced(cfg) {
		return true
	}
	for _, entry := range cfg.Providers.ExternalCredentials {
		if entry != nil && !isLazyDefaultExternalCredentialsEntry(entry) {
			return true
		}
	}
	return false
}

func externalCredentialsUsageReferenced(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	for _, conn := range cfg.Connections {
		if connectionRequiresExternalCredentials(conn) {
			return true
		}
	}
	for _, app := range cfg.Apps {
		if providerEntryRequiresExternalCredentials(app) {
			return true
		}
	}
	for _, entry := range cfg.Providers.Workflow {
		if providerEntryRequiresExternalCredentials(entry) {
			return true
		}
	}
	for _, entry := range cfg.Providers.Agent {
		if providerEntryRequiresExternalCredentials(entry) {
			return true
		}
	}
	for _, definition := range cfg.Workflows.Definitions {
		if workflowDefinitionRequiresExternalCredentials(definition) {
			return true
		}
	}
	if len(cfg.Providers.Agent) > 0 {
		return true
	}
	return false
}

func isLazyDefaultExternalCredentialsEntry(entry *ProviderEntry) bool {
	if entry == nil {
		return false
	}
	want := DefaultProviderSource(DefaultExternalCredentialsProvider, DefaultExternalCredentialsVersion)
	return entry.Source.MetadataURL() == want.MetadataURL() && entry.Source.Path == "" && entry.Source.Builtin == ""
}

func providerEntryRequiresExternalCredentials(entry *ProviderEntry) bool {
	if entry == nil {
		return false
	}
	for _, conn := range entry.Connections {
		if connectionRequiresExternalCredentials(conn) {
			return true
		}
	}
	plan, err := BuildStaticConnectionPlan(entry, entry.ManifestSpec())
	if err != nil {
		return false
	}
	return plan.ConnectionMode() == core.ConnectionModeSubject
}

func connectionRequiresExternalCredentials(conn *ConnectionDef) bool {
	if conn == nil {
		return false
	}
	return ConnectionModeForConnection(*conn) == core.ConnectionModeSubject
}

func workflowDefinitionRequiresExternalCredentials(definition WorkflowDefinitionConfig) bool {
	for _, step := range definition.Steps {
		if step.App != nil && workflowAppCallRequiresExternalCredentials(*step.App) {
			return true
		}
		if step.Agent != nil {
			for _, tool := range step.Agent.Tools {
				if workflowAgentToolRequiresExternalCredentials(tool) {
					return true
				}
			}
		}
	}
	return false
}

func workflowAppCallRequiresExternalCredentials(app WorkflowStepAppCallConfig) bool {
	switch core.NormalizeOptionalConnectionMode(core.ConnectionMode(app.CredentialMode)) {
	case core.ConnectionModeSubject:
		return true
	case core.ConnectionModeNone:
		return false
	default:
		return strings.TrimSpace(app.Connection) != ""
	}
}

func workflowAgentToolRequiresExternalCredentials(tool WorkflowAgentToolRef) bool {
	switch core.NormalizeOptionalConnectionMode(core.ConnectionMode(tool.CredentialMode)) {
	case core.ConnectionModeSubject:
		return true
	case core.ConnectionModeNone:
		return false
	default:
		return strings.TrimSpace(tool.Connection) != ""
	}
}

// IndexedDBIsReferenced reports whether runtime needs a top-level IndexedDB
// provider: explicit selection, configured entries, app bindings, or
// authorization/workflow/agent providers that declare indexeddb usage.
func IndexedDBIsReferenced(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	if strings.TrimSpace(cfg.Server.Providers.IndexedDB) != "" {
		return true
	}
	if len(cfg.Providers.IndexedDB) > 0 {
		return true
	}
	if strings.TrimSpace(cfg.Server.Providers.Authorization) != "" || len(cfg.Providers.Authorization) > 0 {
		return true
	}
	for _, entry := range cfg.Apps {
		if entry != nil && entry.IndexedDB != nil {
			return true
		}
	}
	for _, entry := range cfg.Providers.Workflow {
		if entry != nil && entry.IndexedDB != nil {
			return true
		}
	}
	for _, entry := range cfg.Providers.Agent {
		if entry != nil && entry.IndexedDB != nil {
			return true
		}
	}
	return false
}
