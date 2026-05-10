package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	TenantSettingsSourceConfig      = "config"
	TenantScopeSourceRequestContext = "request_context"
	TenantScopeStorageColumn        = "column"
	TenantScopeStorageNamespace     = "namespace"
)

type TenantResolution struct {
	ID     string
	Host   string
	Tenant *TenantConfig
}

type TenantResolver struct {
	hosts map[string]TenantResolution
}

func NewTenantResolver(tenants map[string]*TenantConfig) (*TenantResolver, error) {
	resolver := &TenantResolver{hosts: map[string]TenantResolution{}}
	for id, tenant := range tenants {
		id = strings.TrimSpace(id)
		if !validTenantID(id) {
			return nil, fmt.Errorf("config validation: tenants.%s id must contain only letters, numbers, underscores, or dashes", id)
		}
		if tenant == nil {
			return nil, fmt.Errorf("config validation: tenants.%s is required", id)
		}
		if len(tenant.Hosts) == 0 {
			return nil, fmt.Errorf("config validation: tenants.%s.hosts is required", id)
		}
		seen := map[string]struct{}{}
		for i, rawHost := range tenant.Hosts {
			host := CanonicalTenantHost(rawHost)
			if host == "" {
				return nil, fmt.Errorf("config validation: tenants.%s.hosts[%d] is required", id, i)
			}
			if _, ok := seen[host]; ok {
				return nil, fmt.Errorf("config validation: tenants.%s.hosts[%d] duplicates %q", id, i, host)
			}
			seen[host] = struct{}{}
			if existing, ok := resolver.hosts[host]; ok {
				return nil, fmt.Errorf("config validation: tenants.%s.hosts[%d] %q conflicts with tenants.%s", id, i, host, existing.ID)
			}
			tenant.Hosts[i] = host
			resolver.hosts[host] = TenantResolution{
				ID:     id,
				Host:   host,
				Tenant: tenant,
			}
		}
	}
	return resolver, nil
}

func (r *TenantResolver) Empty() bool {
	return r == nil || len(r.hosts) == 0
}

func (r *TenantResolver) ResolveHost(rawHost string) (TenantResolution, bool) {
	if r == nil {
		return TenantResolution{}, false
	}
	host := CanonicalTenantHost(rawHost)
	if host == "" {
		return TenantResolution{}, false
	}
	resolved, ok := r.hosts[host]
	return resolved, ok
}

func (c *Config) TenantForHost(rawHost string) (TenantResolution, bool, error) {
	if c == nil {
		return TenantResolution{}, false, nil
	}
	resolver, err := NewTenantResolver(c.Tenants)
	if err != nil {
		return TenantResolution{}, false, err
	}
	resolved, ok := resolver.ResolveHost(rawHost)
	return resolved, ok, nil
}

func CanonicalTenantHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		raw = parsed.Host
	}
	host := raw
	if split, _, err := net.SplitHostPort(raw); err == nil {
		host = split
	} else if h, _, ok := strings.Cut(raw, ":"); ok && !strings.Contains(h, "]") && !strings.Contains(h, "[") {
		host = h
	}
	host = strings.Trim(host, "[]")
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	return host
}

func (c *Config) ProviderTenantSettings(kind HostProviderKind, name string, entry *ProviderEntry) (TenantSettingsConfig, bool, error) {
	return providerTenantSettings(kind, name, entry)
}

func providerTenantSettings(kind HostProviderKind, name string, entry *ProviderEntry) (TenantSettingsConfig, bool, error) {
	var out TenantSettingsConfig
	node := providerConfigMappingValue(entry, "tenantSettings")
	if node == nil {
		return out, false, nil
	}
	if err := node.Decode(&out); err != nil {
		return out, true, fmt.Errorf("config validation: providers.%s.%s.config.tenantSettings: %w", kind, name, err)
	}
	out.Source = strings.TrimSpace(out.Source)
	return out, true, nil
}

func (c *Config) ProviderTenantScope(kind HostProviderKind, name string, entry *ProviderEntry) (TenantScopeConfig, bool, error) {
	return providerTenantScope(kind, name, entry)
}

func providerTenantScope(kind HostProviderKind, name string, entry *ProviderEntry) (TenantScopeConfig, bool, error) {
	var out TenantScopeConfig
	node := providerConfigMappingValue(entry, "tenantScope")
	if node == nil {
		return out, false, nil
	}
	if err := node.Decode(&out); err != nil {
		return out, true, fmt.Errorf("config validation: providers.%s.%s.config.tenantScope: %w", kind, name, err)
	}
	out.Source = strings.TrimSpace(out.Source)
	if out.Storage != nil {
		out.Storage.Strategy = strings.TrimSpace(out.Storage.Strategy)
		out.Storage.Column = strings.TrimSpace(out.Storage.Column)
		out.Storage.NamespaceTemplate = strings.TrimSpace(out.Storage.NamespaceTemplate)
	}
	return out, true, nil
}

func providerConfigMappingValue(entry *ProviderEntry, key string) *yaml.Node {
	if entry == nil || entry.Config.Kind == 0 {
		return nil
	}
	return mappingValueNode(&entry.Config, key)
}

func validTenantID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
