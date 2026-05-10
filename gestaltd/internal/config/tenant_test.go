package config

import (
	"strings"
	"testing"
)

func TestTenantResolverCanonicalizesHosts(t *testing.T) {
	t.Parallel()

	resolver, err := NewTenantResolver(map[string]*TenantConfig{
		"acme": {Hosts: []string{"Acme.Dev.Valon.Tools:443"}},
		"vt":   {Hosts: []string{"https://vt.dev.valon.tools"}},
	})
	if err != nil {
		t.Fatalf("NewTenantResolver: %v", err)
	}

	resolved, ok := resolver.ResolveHost("ACME.dev.valon.tools")
	if !ok {
		t.Fatal("ResolveHost ok = false, want true")
	}
	if resolved.ID != "acme" {
		t.Fatalf("resolved tenant = %q, want acme", resolved.ID)
	}
	if resolved.Host != "acme.dev.valon.tools" {
		t.Fatalf("resolved host = %q, want acme.dev.valon.tools", resolved.Host)
	}
}

func TestTenantResolverRejectsDuplicateHosts(t *testing.T) {
	t.Parallel()

	_, err := NewTenantResolver(map[string]*TenantConfig{
		"acme": {Hosts: []string{"shared.dev.valon.tools"}},
		"vt":   {Hosts: []string{"SHARED.dev.valon.tools"}},
	})
	if err == nil {
		t.Fatal("NewTenantResolver error = nil, want duplicate host error")
	}
	if !strings.Contains(err.Error(), `conflicts with tenants.`) {
		t.Fatalf("NewTenantResolver error = %v, want tenant host conflict", err)
	}
}

func TestLoadConfigParsesTenantsAndTenantProviderConfig(t *testing.T) {
	t.Parallel()

	path := mustWriteConfigFile(t, `
tenants:
  vt:
    hosts:
      - vt.dev.valon.tools
    auth:
      oidc:
        redirectUrl: https://vt.dev.valon.tools/auth/callback
        allowedDomains:
          - valon.com
  acme:
    hosts:
      - acme.dev.valon.tools
tenantPluginConfig:
  indexeddb: main
  objectStore: tenant_plugin_config
providers:
  indexeddb:
    main:
      source:
        package: github.com/valon-technologies/gestalt-providers/indexeddb/relationaldb
        version: 0.0.1-alpha.4
      config:
        tenantScope:
          source: request_context
          storage:
            strategy: column
            column: tenant_id
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok, err := cfg.TenantForHost("acme.dev.valon.tools"); err != nil {
		t.Fatalf("TenantForHost: %v", err)
	} else if !ok {
		t.Fatal("TenantForHost(acme.dev.valon.tools) ok = false, want true")
	}
	if cfg.TenantPluginConfig == nil || cfg.TenantPluginConfig.IndexedDB != "main" {
		t.Fatalf("TenantPluginConfig = %#v, want indexeddb main", cfg.TenantPluginConfig)
	}
	scope, ok, err := cfg.ProviderTenantScope(HostProviderKindIndexedDB, "main", cfg.Providers.IndexedDB["main"])
	if err != nil {
		t.Fatalf("ProviderTenantScope: %v", err)
	}
	if !ok {
		t.Fatal("ProviderTenantScope ok = false, want true")
	}
	if scope.Source != TenantScopeSourceRequestContext {
		t.Fatalf("tenantScope.source = %q, want request_context", scope.Source)
	}
	if scope.Storage == nil || scope.Storage.Column != "tenant_id" {
		t.Fatalf("tenantScope.storage = %#v, want tenant_id column", scope.Storage)
	}
}

func TestTenantForHostReturnsResolverError(t *testing.T) {
	t.Parallel()

	cfg := &Config{Tenants: map[string]*TenantConfig{
		"acme": {Hosts: []string{"shared.dev.valon.tools"}},
		"vt":   {Hosts: []string{"shared.dev.valon.tools"}},
	}}
	_, ok, err := cfg.TenantForHost("shared.dev.valon.tools")
	if err == nil {
		t.Fatal("TenantForHost error = nil, want duplicate host error")
	}
	if ok {
		t.Fatal("TenantForHost ok = true, want false on resolver error")
	}
	if !strings.Contains(err.Error(), `conflicts with tenants.`) {
		t.Fatalf("TenantForHost error = %v, want tenant host conflict", err)
	}
}

func TestLoadConfigValidatesWorkflowAndAgentTenantProviderConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		section string
		source  string
		want    string
	}{
		{
			name:    "workflow",
			section: "workflow",
			source:  "./providers/workflow/temporal",
			want:    `providers.workflow.temporal.config.tenantScope.source "invalid" is not supported`,
		},
		{
			name:    "agent",
			section: "agent",
			source:  "./providers/agent/simple",
			want:    `providers.agent.temporal.config.tenantScope.source "invalid" is not supported`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := mustWriteConfigFile(t, `
providers:
  `+tc.section+`:
    temporal:
      source:
        path: `+tc.source+`
      config:
        tenantScope:
          source: invalid
`)
			_, err := Load(path)
			if err == nil {
				t.Fatal("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
