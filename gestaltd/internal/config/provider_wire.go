package config

import (
	"bytes"
	"fmt"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
	"gopkg.in/yaml.v3"
)

type providerWire struct {
	DisplayName       string                             `yaml:"display_name"`
	Description       string                             `yaml:"description"`
	IconFile          string                             `yaml:"icon_file"`
	Config            yaml.Node                          `yaml:"config"`
	BaseURL           string                             `yaml:"base_url"`
	From              providerSourceWire                 `yaml:"from"`
	Connections       map[string]*providerConnectionWire `yaml:"connections"`
	Headers           map[string]string                  `yaml:"headers"`
	ManagedParameters []ManagedParameterDef              `yaml:"managed_parameters"`
	ResponseMapping   *ResponseMappingDef                `yaml:"response_mapping"`
	AllowedOperations map[string]*OperationOverride      `yaml:"allowed_operations"`
	Surfaces          providerSurfacesWire               `yaml:"surfaces"`
	MCP               *providerMCPWire                   `yaml:"mcp"`
}

type providerSourceWire struct {
	Command      string            `yaml:"command"`
	Manifest     string            `yaml:"manifest"`
	Package      string            `yaml:"package"`
	Source       string            `yaml:"source"`
	Version      string            `yaml:"version"`
	Args         []string          `yaml:"args"`
	Env          map[string]string `yaml:"env"`
	AllowedHosts []string          `yaml:"allowed_hosts"`
}

type providerConnectionWire struct {
	Mode      string                        `yaml:"mode"`
	Auth      ConnectionAuthDef             `yaml:"auth"`
	Params    map[string]ConnectionParamDef `yaml:"params"`
	Discovery *providerDiscoveryWire        `yaml:"discovery"`
}

type providerDiscoveryWire struct {
	URL      string            `yaml:"url"`
	IDPath   string            `yaml:"id_path"`
	NamePath string            `yaml:"name_path"`
	Metadata map[string]string `yaml:"metadata"`
}

type providerSurfacesWire struct {
	REST    *providerRESTSurfaceWire    `yaml:"rest"`
	OpenAPI *providerOpenAPISurfaceWire `yaml:"openapi"`
	GraphQL *providerGraphQLSurfaceWire `yaml:"graphql"`
	MCP     *providerMCPSurfaceWire     `yaml:"mcp"`
}

type providerRESTSurfaceWire struct {
	Connection string               `yaml:"connection"`
	BaseURL    string               `yaml:"base_url"`
	Operations []InlineOperationDef `yaml:"operations"`
}

type providerOpenAPISurfaceWire struct {
	Connection string `yaml:"connection"`
	Document   string `yaml:"document"`
	BaseURL    string `yaml:"base_url"`
}

type providerGraphQLSurfaceWire struct {
	Connection string `yaml:"connection"`
	URL        string `yaml:"url"`
}

type providerMCPSurfaceWire struct {
	Connection string `yaml:"connection"`
	URL        string `yaml:"url"`
}

type providerMCPWire struct {
	Enabled    bool   `yaml:"enabled"`
	ToolPrefix string `yaml:"tool_prefix"`
}

func (i *IntegrationDef) UnmarshalYAML(value *yaml.Node) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var wire providerWire
	if err := dec.Decode(&wire); err != nil {
		return err
	}
	if err := validateProviderWire(&wire); err != nil {
		return err
	}

	plugin := &PluginDef{
		Command:           wire.From.Command,
		Manifest:          wire.From.Manifest,
		Package:           wire.From.Package,
		Source:            wire.From.Source,
		Version:           wire.From.Version,
		Args:              wire.From.Args,
		Env:               wire.From.Env,
		Config:            wire.Config,
		AllowedHosts:      wire.From.AllowedHosts,
		Headers:           wire.Headers,
		ManagedParameters: wire.ManagedParameters,
		ResponseMapping:   wire.ResponseMapping,
		AllowedOperations: wire.AllowedOperations,
	}
	if wire.MCP != nil {
		plugin.MCP = wire.MCP.Enabled
	}

	defaultConn, hasDefault := wire.Connections["default"]
	if hasDefault && defaultConn != nil {
		auth := defaultConn.Auth
		plugin.Auth = &auth
		plugin.ConnectionMode = defaultConn.Mode
		plugin.ConnectionParams = defaultConn.Params
		plugin.PostConnectDiscovery = defaultConn.toManifestDiscovery()
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
				Mode: conn.Mode,
				Auth: conn.Auth,
			}
		}
		if len(plugin.Connections) == 0 {
			plugin.Connections = nil
		}
	}

	if wire.Surfaces.REST != nil {
		plugin.BaseURL = wire.Surfaces.REST.BaseURL
		plugin.Operations = wire.Surfaces.REST.Operations
		if defaultConnection := remapWireConnectionName(wire.Surfaces.REST.Connection); defaultConnection != "" {
			plugin.DefaultConnection = defaultConnection
		} else if hasDefault && len(plugin.Connections) > 0 {
			plugin.DefaultConnection = PluginConnectionAlias
		}
	}
	if wire.Surfaces.OpenAPI != nil {
		plugin.OpenAPI = wire.Surfaces.OpenAPI.Document
		if wire.Surfaces.OpenAPI.BaseURL != "" {
			plugin.BaseURL = wire.Surfaces.OpenAPI.BaseURL
		}
		plugin.OpenAPIConnection = remapWireConnectionName(wire.Surfaces.OpenAPI.Connection)
	}
	if wire.Surfaces.GraphQL != nil {
		plugin.GraphQLURL = wire.Surfaces.GraphQL.URL
		plugin.GraphQLConnection = remapWireConnectionName(wire.Surfaces.GraphQL.Connection)
	}
	if wire.Surfaces.MCP != nil {
		plugin.MCPURL = wire.Surfaces.MCP.URL
		plugin.MCPConnection = remapWireConnectionName(wire.Surfaces.MCP.Connection)
	}
	if wire.BaseURL != "" {
		plugin.BaseURL = wire.BaseURL
	}

	*i = IntegrationDef{
		Plugin:      plugin,
		DisplayName: wire.DisplayName,
		Description: wire.Description,
		IconFile:    wire.IconFile,
	}
	if wire.MCP != nil {
		i.MCPToolPrefix = wire.MCP.ToolPrefix
	}
	return nil
}

func validateProviderWire(wire *providerWire) error {
	apiSurfaceCount := 0
	if wire.Surfaces.REST != nil {
		apiSurfaceCount++
	}
	if wire.Surfaces.OpenAPI != nil {
		apiSurfaceCount++
	}
	if wire.Surfaces.GraphQL != nil {
		apiSurfaceCount++
	}
	if apiSurfaceCount > 1 {
		return fmt.Errorf("provider config can define only one of surfaces.rest, surfaces.openapi, or surfaces.graphql")
	}
	if wire.Surfaces.REST != nil && wire.Surfaces.REST.BaseURL == "" {
		return fmt.Errorf("surfaces.rest.base_url is required when surfaces.rest is configured")
	}
	if wire.Surfaces.REST != nil && len(wire.Surfaces.REST.Operations) == 0 {
		return fmt.Errorf("surfaces.rest.operations is required when surfaces.rest is configured")
	}
	if wire.Surfaces.OpenAPI != nil && wire.Surfaces.OpenAPI.Document == "" {
		return fmt.Errorf("surfaces.openapi.document is required when surfaces.openapi is configured")
	}
	if wire.Surfaces.GraphQL != nil && wire.Surfaces.GraphQL.URL == "" {
		return fmt.Errorf("surfaces.graphql.url is required when surfaces.graphql is configured")
	}
	if wire.Surfaces.MCP != nil && wire.Surfaces.MCP.URL == "" {
		return fmt.Errorf("surfaces.mcp.url is required when surfaces.mcp is configured")
	}
	if wire.MCP != nil && wire.MCP.ToolPrefix != "" && !wire.MCP.Enabled {
		return fmt.Errorf("mcp.tool_prefix is only valid when mcp.enabled is true")
	}

	hasDefaultConnection := false
	hasNamedConnections := false
	for name, conn := range wire.Connections {
		if conn == nil {
			if name == "default" {
				hasDefaultConnection = true
			} else {
				hasNamedConnections = true
			}
			continue
		}
		if name == "default" {
			hasDefaultConnection = true
		} else {
			hasNamedConnections = true
		}
		if name != "default" {
			if len(conn.Params) > 0 {
				return fmt.Errorf("connections.%s.params are only supported on connections.default", name)
			}
			if conn.Discovery != nil {
				return fmt.Errorf("connections.%s.discovery is only supported on connections.default", name)
			}
		}
	}

	if hasNamedConnections && !hasDefaultConnection {
		if wire.Surfaces.REST != nil && wire.Surfaces.REST.Connection == "" {
			return fmt.Errorf("surfaces.rest.connection is required when using named connections without connections.default")
		}
		if wire.Surfaces.OpenAPI != nil && wire.Surfaces.OpenAPI.Connection == "" {
			return fmt.Errorf("surfaces.openapi.connection is required when using named connections without connections.default")
		}
		if wire.Surfaces.GraphQL != nil && wire.Surfaces.GraphQL.Connection == "" {
			return fmt.Errorf("surfaces.graphql.connection is required when using named connections without connections.default")
		}
		if wire.Surfaces.MCP != nil && wire.Surfaces.MCP.Connection == "" {
			return fmt.Errorf("surfaces.mcp.connection is required when using named connections without connections.default")
		}
	}

	checkConnectionRef := func(fieldPath, name string) error {
		if name == "" {
			return nil
		}
		if _, ok := wire.Connections[name]; !ok {
			return fmt.Errorf("%s references undeclared connection %q", fieldPath, name)
		}
		return nil
	}
	if wire.Surfaces.REST != nil {
		if err := checkConnectionRef("surfaces.rest.connection", wire.Surfaces.REST.Connection); err != nil {
			return err
		}
	}
	if wire.Surfaces.OpenAPI != nil {
		if err := checkConnectionRef("surfaces.openapi.connection", wire.Surfaces.OpenAPI.Connection); err != nil {
			return err
		}
	}
	if wire.Surfaces.GraphQL != nil {
		if err := checkConnectionRef("surfaces.graphql.connection", wire.Surfaces.GraphQL.Connection); err != nil {
			return err
		}
	}
	if wire.Surfaces.MCP != nil {
		if err := checkConnectionRef("surfaces.mcp.connection", wire.Surfaces.MCP.Connection); err != nil {
			return err
		}
	}

	return nil
}

func (w *providerConnectionWire) toManifestDiscovery() *pluginmanifestv1.ProviderPostConnectDiscovery {
	if w == nil || w.Discovery == nil {
		return nil
	}
	return &pluginmanifestv1.ProviderPostConnectDiscovery{
		URL:             w.Discovery.URL,
		IDPath:          w.Discovery.IDPath,
		NamePath:        w.Discovery.NamePath,
		MetadataMapping: w.Discovery.Metadata,
	}
}

func remapWireConnectionName(name string) string {
	switch name {
	case "", "default":
		return ""
	default:
		return name
	}
}
