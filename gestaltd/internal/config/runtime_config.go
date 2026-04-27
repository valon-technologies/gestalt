package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

type componentRuntimeConfig struct {
	Name         string                `yaml:"name"`
	Source       *ProviderSource       `yaml:"source,omitempty"`
	Command      string                `yaml:"command"`
	Args         []string              `yaml:"args,omitempty"`
	Env          map[string]string     `yaml:"env,omitempty"`
	Egress       *ProviderEgressConfig `yaml:"egress,omitempty"`
	HostBinary   string                `yaml:"hostBinary,omitempty"`
	ManifestPath string                `yaml:"manifestPath,omitempty"`
	Config       yaml.Node             `yaml:"config,omitempty"`
}

func BuildComponentRuntimeConfigNode(name, kind string, entry *ProviderEntry, providerConfig yaml.Node) (yaml.Node, error) {
	if entry == nil {
		return yaml.Node{}, fmt.Errorf("%s %q provider is required", kind, name)
	}
	node := componentRuntimeConfig{
		Name: name,
		Source: &ProviderSource{
			Path:          entry.Source.Path,
			metadataURL:   entry.Source.MetadataURL(),
			GitHubRelease: entry.Source.GitHubReleaseSource(),
		},
		Command:      entry.Command,
		Args:         append([]string(nil), entry.Args...),
		Env:          entry.Env,
		Egress:       cloneProviderEgressConfig(entry.Egress),
		HostBinary:   entry.HostBinary,
		ManifestPath: entry.ResolvedManifestPath,
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
	for _, key := range []string{"source", "command", "manifestPath", "hostBinary"} {
		if mappingValueNode(raw, key) != nil {
			return true
		}
	}
	return false
}
