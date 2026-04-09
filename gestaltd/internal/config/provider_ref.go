package config

import (
	"bytes"
	"fmt"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

type pluginDefWire struct {
	Source       *PluginSourceDef  `yaml:"source"`
	Env          map[string]string `yaml:"env"`
	AllowedHosts []string          `yaml:"allowedHosts"`
}

type integrationWire struct {
	DisplayName       string                            `yaml:"displayName"`
	Description       string                            `yaml:"description"`
	IconFile          string                            `yaml:"iconFile"`
	Provider          *ProviderDef                      `yaml:"provider"`
	Config            yaml.Node                         `yaml:"config"`
	Connections       map[string]*integrationConnection `yaml:"connections"`
	AllowedOperations map[string]*OperationOverride     `yaml:"allowedOperations"`
	MCP               *integrationMCPWire               `yaml:"mcp"`
	Datastores        map[string]string                 `yaml:"datastores"`
}

type integrationConnection struct {
	Mode      string                        `yaml:"mode"`
	Auth      ConnectionAuthDef             `yaml:"auth"`
	Params    map[string]ConnectionParamDef `yaml:"params"`
	Discovery *integrationDiscoveryWire     `yaml:"discovery"`
}

type integrationDiscoveryWire struct {
	URL      string            `yaml:"url"`
	IDPath   string            `yaml:"idPath"`
	NamePath string            `yaml:"namePath"`
	Metadata map[string]string `yaml:"metadata"`
}

type integrationMCPWire struct {
	Enabled    bool   `yaml:"enabled"`
	ToolPrefix string `yaml:"toolPrefix"`
}

func (p *ProviderDef) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		*p = ProviderDef{}
		return nil
	}
	if value.Kind == yaml.ScalarNode {
		return fmt.Errorf("provider must be a mapping with source/env/allowed_hosts fields")
	}
	if value.Kind != yaml.MappingNode {
		var probe map[string]any
		return value.Decode(&probe)
	}

	var wire pluginDefWire
	if err := decodeKnownYAMLNode(value, &wire); err != nil {
		return err
	}
	*p = ProviderDef{
		Source:       wire.Source,
		Env:          wire.Env,
		AllowedHosts: wire.AllowedHosts,
	}
	return nil
}

func (i *IntegrationDef) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		*i = IntegrationDef{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		var probe map[string]any
		return value.Decode(&probe)
	}

	var wire integrationWire
	if err := decodeKnownYAMLNode(value, &wire); err != nil {
		return err
	}
	if err := validateIntegrationWire(&wire); err != nil {
		return err
	}

	plugin := wire.Provider
	if plugin != nil {
		plugin.Config = wire.Config
		plugin.AllowedOperations = wire.AllowedOperations
		if wire.MCP != nil {
			plugin.MCP = wire.MCP.Enabled
		}
		if defaultConn, ok := wire.Connections["default"]; ok && defaultConn != nil {
			auth := defaultConn.Auth
			plugin.Auth = &auth
			plugin.ConnectionMode = defaultConn.Mode
			plugin.ConnectionParams = defaultConn.Params
			plugin.Discovery = defaultConn.toManifestDiscovery()
		}
		if len(wire.Connections) > 0 {
			plugin.Connections = make(map[string]*ConnectionDef, len(wire.Connections))
			for name, conn := range wire.Connections {
				if name == "default" {
					continue
				}
				if conn == nil {
					plugin.Connections[name] = nil
					continue
				}
				plugin.Connections[name] = &ConnectionDef{
					Mode:             conn.Mode,
					Auth:             conn.Auth,
					ConnectionParams: conn.Params,
					Discovery:        conn.toManifestDiscovery(),
				}
			}
			if len(plugin.Connections) == 0 {
				plugin.Connections = nil
			}
		}
	}

	*i = IntegrationDef{
		Plugin:      plugin,
		DisplayName: wire.DisplayName,
		Description: wire.Description,
		IconFile:    wire.IconFile,
		Datastores:  wire.Datastores,
	}
	if wire.MCP != nil {
		i.MCPToolPrefix = wire.MCP.ToolPrefix
	}
	return nil
}

func decodeKnownYAMLNode(value *yaml.Node, out any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(out)
}

func validateIntegrationWire(wire *integrationWire) error {
	if wire.MCP != nil && wire.MCP.ToolPrefix != "" && !wire.MCP.Enabled {
		return fmt.Errorf("mcp.toolPrefix is only valid when mcp.enabled is true")
	}

	return nil
}

func (w *integrationConnection) toManifestDiscovery() *pluginmanifestv1.ProviderDiscovery {
	if w == nil || w.Discovery == nil {
		return nil
	}
	return &pluginmanifestv1.ProviderDiscovery{
		URL:      w.Discovery.URL,
		IDPath:   w.Discovery.IDPath,
		NamePath: w.Discovery.NamePath,
		Metadata: w.Discovery.Metadata,
	}
}
