package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

type componentRuntimeConfig struct {
	Name         string            `yaml:"name"`
	Source       *PluginSourceDef  `yaml:"source,omitempty"`
	Command      string            `yaml:"command"`
	Args         []string          `yaml:"args,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`
	AllowedHosts []string          `yaml:"allowed_hosts,omitempty"`
	HostBinary   string            `yaml:"host_binary,omitempty"`
	ManifestPath string            `yaml:"manifest_path,omitempty"`
	Config       yaml.Node         `yaml:"config,omitempty"`
}

func sanitizeRuntimeSource(src *PluginSourceDef) *PluginSourceDef {
	if src == nil {
		return nil
	}
	return &PluginSourceDef{
		Path:    src.Path,
		Ref:     src.Ref,
		Version: src.Version,
	}
}

func BuildComponentRuntimeConfigNode(name, kind string, provider *PluginDef, providerConfig yaml.Node) (yaml.Node, error) {
	if provider == nil {
		return yaml.Node{}, fmt.Errorf("%s %q provider is required", kind, name)
	}
	node := componentRuntimeConfig{
		Name:         name,
		Source:       sanitizeRuntimeSource(provider.Source),
		Command:      provider.Command,
		Args:         append([]string(nil), provider.Args...),
		Env:          provider.Env,
		AllowedHosts: provider.AllowedHosts,
		HostBinary:   provider.HostBinary,
		ManifestPath: provider.ResolvedManifestPath,
		Config:       providerConfig,
	}
	data, err := yaml.Marshal(node)
	if err != nil {
		return yaml.Node{}, fmt.Errorf("marshal %s %q runtime config: %w", kind, name, err)
	}
	var out yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&out); err != nil {
		return yaml.Node{}, fmt.Errorf("decode %s %q runtime config: %w", kind, name, err)
	}
	if out.Kind == yaml.DocumentNode && len(out.Content) == 1 {
		return *out.Content[0], nil
	}
	return out, nil
}

func IsComponentRuntimeConfigNode(node yaml.Node) bool {
	raw := documentValueNode(&node)
	if raw == nil || raw.Kind != yaml.MappingNode {
		return false
	}
	if mappingValueNode(raw, "config") == nil {
		return false
	}
	for _, key := range []string{"source", "command", "manifest_path", "host_binary"} {
		if mappingValueNode(raw, key) != nil {
			return true
		}
	}
	return false
}
